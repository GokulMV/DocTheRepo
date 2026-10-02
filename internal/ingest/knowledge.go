package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// KnowledgeDocs stores synced documents (implemented by store.Knowledge).
type KnowledgeDocs interface {
	MentionIndex(ctx context.Context) (*palace.MentionIndex, error)
	Apply(ctx context.Context, connectorID string, docs []ports.KnowledgeDoc, mentions *palace.MentionIndex) (ports.KnowledgeApplied, error)
	RemoveMissing(ctx context.Context, connectorID, space string, live []string) (int, error)
}


// UpstreamRules stores known-issue rules tied to Jira issues and Confluence pages (store.KnownIssues).
type UpstreamRules interface {
	UpstreamRules(ctx context.Context, source string, refs []string, labelManaged bool) (map[string]ports.UpstreamRule, error)
	CreateImported(ctx context.Context, r ports.ImportedRule) (string, error)
	SetUpstream(ctx context.Context, id, status, action, note string) error
}

// Proposal is a proposed rule for a labelled document (suggest.FromText's result, reduced).
type Proposal struct {
	Explanation string
	Reason      string
	Match       json.RawMessage
}

// KnowledgeSync schedules and runs Confluence/Jira syncs (plan § 8.10, § 8.16): changed documents are
// chunked, embedded, linked into the Palace, and shelved; documents deleted upstream are removed daily;
// documents carrying the known-issue label become draft rules; and a Jira issue moving to Done (or losing
// the label) flips its rule to label_only, so a fixed bug's errors are no longer hidden.
type KnowledgeSync struct {
	Store   PollStore
	Queue   Enqueuer
	Sources map[string]ports.KnowledgeSource
	Docs    KnowledgeDocs
	Rules   UpstreamRules
	// Embed embeds changed chunks (pipeline.Indexer.Embed); nil skips embedding.
	Embed func(ctx context.Context, meta llmgateway.CallMeta, chunks []ports.Chunk) (int, error)
	// Propose drafts a rule from a labelled document's text (suggest route); nil or an error leaves the
	// draft without a match.
	Propose func(ctx context.Context, text string) (Proposal, error)
	// ReloadRules recompiles the matcher after rule actions change.
	ReloadRules func(ctx context.Context) error
	Now         func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

// Knowledge sync defaults.
const (
	DefaultKnowledgeInterval = 15 * time.Minute
	DefaultKnownIssueLabel   = "known-issue"
	reconcileEvery           = 24 * time.Hour
	reconcileStream          = "~reconciled"
	// MaxSourceText caps the upstream text kept on an imported rule.
	MaxSourceText = 50_000
)

// Notes written on rules changed by a sync.
const (
	NoteFixedUpstream = "Fixed upstream (Jira issue is Done) — verify the errors stopped, then delete the rule or turn suppression back on."
	NoteLabelRemoved  = "The known-issue label was removed upstream — verify, then delete the rule or turn suppression back on."
)

// SyncPayload is a knowledge_sync job's payload.
type SyncPayload struct {
	ConnectorID string `json:"connector_id"`
}

// KnowledgeResult is stored on the job.
type KnowledgeResult struct {
	Docs      int      `json:"docs"`
	Added     int      `json:"chunks_added"`
	Changed   int      `json:"chunks_changed"`
	Removed   int      `json:"chunks_removed"`
	Embedded  int      `json:"embedded"`
	Links     int      `json:"palace_links"`
	Deleted   int      `json:"docs_deleted"`
	Imported  int      `json:"rules_imported"`
	Flipped   int      `json:"rules_flipped"`
	Labeled   int      `json:"labeled_docs"`
	Notes     []string `json:"notes,omitempty"`
	Streams   int      `json:"streams"`
	Reconcile bool     `json:"reconciled"`
}

func (k *KnowledgeSync) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// Enqueue queues a sync for every knowledge connector whose interval has elapsed.
func (k *KnowledgeSync) Enqueue(ctx context.Context) (int, error) {
	types := make([]string, 0, len(k.Sources))
	for t := range k.Sources {
		types = append(types, t)
	}
	ccs, err := k.Store.ListByType(ctx, types...)
	if err != nil {
		return 0, err
	}
	now := k.now()
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.last == nil {
		k.last = map[string]time.Time{}
	}
	n := 0
	var errs []error
	for _, cc := range ccs {
		interval := time.Duration(cc.PollSeconds) * time.Second
		if interval <= 0 {
			interval = DefaultKnowledgeInterval
		}
		if now.Sub(k.last[cc.ID]) < interval {
			continue
		}
		_, created, err := k.Queue.Enqueue(ctx, ports.NewJob{Type: ports.JobKnowledgeSync, SerialKey: "knowledge:" + cc.ID,
			DedupeKey: "knowledge:" + cc.ID, Payload: SyncPayload{ConnectorID: cc.ID}, MaxAttempts: 3})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		k.last[cc.ID] = now
		if created {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Handle runs one sync. Each page of documents is committed before its cursor is stored, so a crash
// repeats at most one page (writes are idempotent).
func (k *KnowledgeSync) Handle(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl SyncPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.ConnectorID == "" {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("knowledge sync payload: %v", err))
	}
	cc, err := k.Store.Get(ctx, pl.ConnectorID)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.Outcome{Status: ports.JobAborted, Message: "connector deleted or disabled"}, nil
	}
	if err != nil {
		return ports.Outcome{}, err
	}
	src, ok := k.Sources[cc.Type]
	if !ok {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("no knowledge source for connector type %q", cc.Type))
	}
	res, err := k.sync(ctx, job, cc, src)
	if herr := k.Store.SetHealth(context.WithoutCancel(ctx), cc.ID, err); herr != nil && err == nil {
		err = herr
	}
	if err != nil {
		var v *ports.ValidationError
		if errors.As(err, &v) {
			return ports.Outcome{}, ports.Permanent(err)
		}
		return ports.Outcome{}, err
	}
	return ports.Outcome{Status: ports.JobDone, Result: res}, nil
}

func (k *KnowledgeSync) sync(ctx context.Context, job ports.Job, cc ports.ConnectorConfig, src ports.KnowledgeSource) (KnowledgeResult, error) {
	var res KnowledgeResult
	streams, err := src.Streams(cc)
	if err != nil {
		return res, err
	}
	cursors, err := k.Store.Cursors(ctx, cc.ID)
	if err != nil {
		return res, err
	}
	idx, err := k.Docs.MentionIndex(ctx)
	if err != nil {
		return res, err
	}
	meta := llmgateway.CallMeta{JobID: job.ID}
	apply := func(docs []ports.KnowledgeDoc) error {
		a, err := k.Docs.Apply(ctx, cc.ID, docs, idx)
		if err != nil {
			return err
		}
		res.Docs, res.Added, res.Changed, res.Removed, res.Links = res.Docs+a.Docs, res.Added+a.Added, res.Changed+a.Changed, res.Removed+a.Removed, res.Links+a.Links
		if k.Embed != nil && len(a.Embed) > 0 {
			n, err := k.Embed(ctx, meta, a.Embed)
			var sb *ports.SpendBlockedError
			switch {
			case errors.As(err, &sb):
				res.Notes = appendOnce(res.Notes, "embedding blocked by the spend guard (documents stay searchable by keyword): "+sb.Error())
			case err != nil:
				return err
			}
			res.Embedded += n
		}
		return nil
	}
	for _, stream := range streams {
		res.Streams++
		err := src.Changed(ctx, cc, stream, cursors[stream], func(docs []ports.KnowledgeDoc, cursor string) error {
			if err := apply(docs); err != nil {
				return err
			}
			n, err := k.upstream(ctx, docs)
			if err != nil {
				return err
			}
			res.Flipped += n
			if cursor == "" || cursor == cursors[stream] {
				return nil
			}
			cursors[stream] = cursor
			return k.Store.SetCursor(ctx, cc.ID, stream, cursor)
		})
		if err != nil {
			return res, fmt.Errorf("sync %s %s: %w", cc.Type, stream, err)
		}
	}

	if rec, ok := src.(ports.KnowledgeReconciler); ok {
		last, _ := time.Parse(time.RFC3339, cursors[reconcileStream])
		if k.now().Sub(last) >= reconcileEvery {
			for _, stream := range streams {
				ids, err := rec.IDs(ctx, cc, stream)
				if err != nil {
					return res, fmt.Errorf("list %s %s: %w", cc.Type, stream, err)
				}
				n, err := k.Docs.RemoveMissing(ctx, cc.ID, stream, ids)
				if err != nil {
					return res, err
				}
				res.Deleted += n
			}
			res.Reconcile = true
			if err := k.Store.SetCursor(ctx, cc.ID, reconcileStream, k.now().UTC().Format(time.RFC3339)); err != nil {
				return res, err
			}
		}
	}

	label := strings.ToLower(strings.TrimSpace(cc.Config["known_issue_label"]))
	if label == "" {
		label = DefaultKnownIssueLabel
	}
	if label != "-" && k.Rules != nil {
		docs, err := src.Labeled(ctx, cc, label)
		if err != nil {
			return res, fmt.Errorf("labelled %s documents: %w", cc.Type, err)
		}
		res.Labeled = len(docs)
		if err := apply(docs); err != nil {
			return res, err
		}
		imported, flipped, notes, err := k.importLabeled(ctx, cc.Type, docs)
		if err != nil {
			return res, err
		}
		res.Imported, res.Flipped = imported, res.Flipped+flipped
		for _, n := range notes {
			res.Notes = appendOnce(res.Notes, n)
		}
	}
	if res.Flipped > 0 && k.ReloadRules != nil {
		if err := k.ReloadRules(ctx); err != nil {
			res.Notes = append(res.Notes, "reload known-issue rules: "+err.Error())
		}
	}
	return res, nil
}

func appendOnce(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// upstream applies upstream state to rules tied to these documents: a suppressing rule whose Jira issue
// is Done flips to label_only (once: a human who turns suppression back on is not overridden).
func (k *KnowledgeSync) upstream(ctx context.Context, docs []ports.KnowledgeDoc) (int, error) {
	if k.Rules == nil {
		return 0, nil
	}
	bySource := map[string][]ports.KnowledgeDoc{}
	for _, d := range docs {
		bySource[string(d.Source)] = append(bySource[string(d.Source)], d)
	}
	flipped := 0
	for source, ds := range bySource {
		refs := make([]string, len(ds))
		for i, d := range ds {
			refs[i] = d.ExternalID
		}
		rules, err := k.Rules.UpstreamRules(ctx, source, refs, false)
		if err != nil {
			return flipped, err
		}
		for _, d := range ds {
			r, ok := rules[d.ExternalID]
			if !ok {
				continue
			}
			action, note := "", ""
			if d.Done && r.Action == "suppress" && r.UpstreamNote == "" {
				action, note = "label_only", NoteFixedUpstream
				flipped++
			}
			if err := k.Rules.SetUpstream(ctx, r.ID, d.Status, action, note); err != nil {
				return flipped, err
			}
		}
	}
	return flipped, nil
}

// importLabeled creates draft rules for newly labelled documents, applies Done flips, and flips
// label-managed rules whose document lost the label.
func (k *KnowledgeSync) importLabeled(ctx context.Context, source string, docs []ports.KnowledgeDoc) (imported, flipped int, notes []string, err error) {
	if flipped, err = k.upstream(ctx, docs); err != nil {
		return
	}
	refs := make([]string, len(docs))
	labeled := map[string]bool{}
	for i, d := range docs {
		refs[i], labeled[d.ExternalID] = d.ExternalID, true
	}
	existing := map[string]ports.UpstreamRule{}
	if len(refs) > 0 {
		if existing, err = k.Rules.UpstreamRules(ctx, source, refs, false); err != nil {
			return
		}
	}
	for _, d := range docs {
		if _, ok := existing[d.ExternalID]; ok {
			continue
		}
		r := ports.ImportedRule{Source: source, Ref: d.ExternalID, Title: d.Title, Description: summary(d.Markdown, 300),
			SourceText: truncateText(d.Markdown, MaxSourceText), TicketURL: d.URL, Reason: "known_bug", Action: "suppress",
			UpstreamStatus: d.Status}
		if d.Done {
			r.Action, r.UpstreamNote = "label_only", NoteFixedUpstream
		}
		if k.Propose != nil {
			p, perr := k.Propose(ctx, d.Title+"\n\n"+d.Markdown)
			switch {
			case perr == nil:
				r.Explanation, r.Match = p.Explanation, p.Match
				if p.Reason != "" {
					r.Reason = p.Reason
				}
			case errors.Is(perr, llmgateway.ErrNoRoute):
				notes = append(notes, "no model is routed to the suggest feature: imported rules have no proposed match yet")
			default:
				var sb *ports.SpendBlockedError
				if errors.As(perr, &sb) {
					notes = append(notes, "rule proposals blocked by the spend guard: "+sb.Error())
				} else {
					notes = append(notes, "rule proposal failed: "+perr.Error())
				}
			}
		}
		id, cerr := k.Rules.CreateImported(ctx, r)
		if cerr != nil {
			err = cerr
			return
		}
		if id != "" {
			imported++
		}
	}
	managed, err := k.Rules.UpstreamRules(ctx, source, nil, true)
	if err != nil {
		return
	}
	for ref, r := range managed {
		if labeled[ref] || r.Action != "suppress" || r.UpstreamNote != "" {
			continue
		}
		if err = k.Rules.SetUpstream(ctx, r.ID, "", "label_only", NoteLabelRemoved); err != nil {
			return
		}
		flipped++
	}
	return
}

func summary(md string, n int) string {
	for _, para := range strings.Split(md, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" || strings.HasPrefix(para, "#") || strings.HasPrefix(para, "**Type:**") {
			continue
		}
		return truncateText(strings.Join(strings.Fields(para), " "), n)
	}
	return ""
}

func truncateText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ErrNoKnowledgeConnector is returned when no Confluence or Jira connector owns a URL.
var ErrNoKnowledgeConnector = errors.New("no Confluence or Jira connector matches this URL")

// FetchLink loads the Jira issue or Confluence page a browser URL points to, through the connector whose
// site owns it (POST /known-issues/from-link).
func (k *KnowledgeSync) FetchLink(ctx context.Context, url string) (ports.KnowledgeDoc, error) {
	types := make([]string, 0, len(k.Sources))
	for t := range k.Sources {
		types = append(types, t)
	}
	ccs, err := k.Store.ListByType(ctx, types...)
	if err != nil {
		return ports.KnowledgeDoc{}, err
	}
	for _, cc := range ccs {
		src := k.Sources[cc.Type]
		if src == nil || !src.Owns(cc, url) {
			continue
		}
		full, err := k.Store.Get(ctx, cc.ID) // ListByType may omit credentials
		if err != nil {
			return ports.KnowledgeDoc{}, err
		}
		return src.Fetch(ctx, full, url)
	}
	return ports.KnowledgeDoc{}, ErrNoKnowledgeConnector
}
