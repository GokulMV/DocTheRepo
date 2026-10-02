package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type fakeKnowledgeSource struct {
	changed map[string][][]ports.KnowledgeDoc // stream → pages
	labeled []ports.KnowledgeDoc
	ids     map[string][]string
	seen    map[string]string // stream → cursor passed in
}

func (f *fakeKnowledgeSource) Type() string { return "jira" }
func (f *fakeKnowledgeSource) Streams(ports.ConnectorConfig) ([]string, error) {
	return []string{"ENG"}, nil
}
func (f *fakeKnowledgeSource) Changed(_ context.Context, _ ports.ConnectorConfig, s, cursor string, emit func([]ports.KnowledgeDoc, string) error) error {
	if f.seen == nil {
		f.seen = map[string]string{}
	}
	f.seen[s] = cursor
	for i, p := range f.changed[s] {
		if err := emit(p, "c"+string(rune('1'+i))); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeKnowledgeSource) Labeled(context.Context, ports.ConnectorConfig, string) ([]ports.KnowledgeDoc, error) {
	return f.labeled, nil
}
func (f *fakeKnowledgeSource) Owns(_ ports.ConnectorConfig, u string) bool {
	return u == "https://acme.atlassian.net/browse/ENG-1"
}
func (f *fakeKnowledgeSource) Fetch(context.Context, ports.ConnectorConfig, string) (ports.KnowledgeDoc, error) {
	return ports.KnowledgeDoc{ExternalID: "ENG-1", Source: ports.SourceJira}, nil
}
func (f *fakeKnowledgeSource) IDs(_ context.Context, _ ports.ConnectorConfig, s string) ([]string, error) {
	return f.ids[s], nil
}

type fakeDocs struct {
	applied []ports.KnowledgeDoc
	removed map[string][]string
}

func (f *fakeDocs) MentionIndex(context.Context) (*palace.MentionIndex, error) {
	return palace.NewMentionIndex(nil), nil
}
func (f *fakeDocs) Apply(_ context.Context, _ string, docs []ports.KnowledgeDoc, _ *palace.MentionIndex) (ports.KnowledgeApplied, error) {
	f.applied = append(f.applied, docs...)
	return ports.KnowledgeApplied{Docs: len(docs), Added: len(docs), Embed: []ports.Chunk{{ID: "c"}}}, nil
}
func (f *fakeDocs) RemoveMissing(_ context.Context, _, space string, live []string) (int, error) {
	if f.removed == nil {
		f.removed = map[string][]string{}
	}
	f.removed[space] = live
	return 1, nil
}

type fakeRules struct {
	rules   map[string]*ports.UpstreamRule
	created []ports.ImportedRule
}

func (f *fakeRules) UpstreamRules(_ context.Context, source string, refs []string, labelManaged bool) (map[string]ports.UpstreamRule, error) {
	out := map[string]ports.UpstreamRule{}
	for ref, r := range f.rules {
		if r.Source != source || (labelManaged && !r.LabelManaged) {
			continue
		}
		if len(refs) > 0 {
			ok := false
			for _, x := range refs {
				ok = ok || x == ref
			}
			if !ok {
				continue
			}
		}
		out[ref] = *r
	}
	return out, nil
}
func (f *fakeRules) CreateImported(_ context.Context, r ports.ImportedRule) (string, error) {
	f.created = append(f.created, r)
	f.rules[r.Ref] = &ports.UpstreamRule{ID: "new-" + r.Ref, Source: r.Source, Ref: r.Ref, Action: r.Action, LabelManaged: true, UpstreamNote: r.UpstreamNote}
	return "new-" + r.Ref, nil
}
func (f *fakeRules) SetUpstream(_ context.Context, id, _, action, note string) error {
	for _, r := range f.rules {
		if r.ID == id && action != "" {
			r.Action, r.UpstreamNote = action, note
		}
	}
	return nil
}

type fakePollStore struct {
	cc      ports.ConnectorConfig
	cursors map[string]string
	health  error
}

func (f *fakePollStore) Get(context.Context, string) (ports.ConnectorConfig, error) { return f.cc, nil }
func (f *fakePollStore) ListByType(context.Context, ...string) ([]ports.ConnectorConfig, error) {
	return []ports.ConnectorConfig{f.cc}, nil
}
func (f *fakePollStore) Cursors(context.Context, string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.cursors {
		out[k] = v
	}
	return out, nil
}
func (f *fakePollStore) SetCursor(_ context.Context, _, stream, c string) error {
	f.cursors[stream] = c
	return nil
}
func (f *fakePollStore) SetHealth(_ context.Context, _ string, err error) error {
	f.health = err
	return nil
}

func doc(key string, done bool, labels ...string) ports.KnowledgeDoc {
	return ports.KnowledgeDoc{Source: ports.SourceJira, ExternalID: key, Space: "ENG", Title: key + ": boom", Markdown: "# " + key + "\n\nPool exhausted.",
		Status: map[bool]string{true: "Done", false: "In Progress"}[done], Done: done, Labels: labels, URL: "https://acme.atlassian.net/browse/" + key}
}

func TestKnowledgeSync(t *testing.T) {
	ctx := context.Background()
	src := &fakeKnowledgeSource{
		changed: map[string][][]ports.KnowledgeDoc{"ENG": {{doc("ENG-1", true)}, {doc("ENG-2", false)}}},
		labeled: []ports.KnowledgeDoc{doc("ENG-2", false, "known-issue"), doc("ENG-3", true, "known-issue")},
		ids:     map[string][]string{"ENG": {"ENG-1", "ENG-2"}},
	}
	rules := &fakeRules{rules: map[string]*ports.UpstreamRule{
		// A rule saved from a link (not label-managed) whose issue moved to Done.
		"ENG-1": {ID: "r1", Source: "jira", Ref: "ENG-1", Action: "suppress", Enabled: true},
		// A label-managed rule whose issue lost the label.
		"ENG-9": {ID: "r9", Source: "jira", Ref: "ENG-9", Action: "suppress", LabelManaged: true},
		// A human turned suppression back on after a flip: not overridden.
		"ENG-8": {ID: "r8", Source: "jira", Ref: "ENG-8", Action: "suppress", LabelManaged: true, UpstreamNote: NoteFixedUpstream},
	}}
	ps := &fakePollStore{cc: ports.ConnectorConfig{ID: "c1", Type: "jira"}, cursors: map[string]string{"ENG": "c0"}}
	docs := &fakeDocs{}
	reloaded := 0
	embedded := 0
	k := &KnowledgeSync{Store: ps, Sources: map[string]ports.KnowledgeSource{"jira": src}, Docs: docs, Rules: rules,
		Embed: func(_ context.Context, _ llmgateway.CallMeta, cs []ports.Chunk) (int, error) {
			embedded += len(cs)
			return len(cs), nil
		},
		Propose: func(_ context.Context, text string) (Proposal, error) {
			return Proposal{Explanation: "Matches the pool errors.", Reason: "in_progress", Match: json.RawMessage(`{"services":["checkout"]}`)}, nil
		},
		ReloadRules: func(context.Context) error { reloaded++; return nil },
		Now:         func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }}

	out, err := k.Handle(ctx, ports.Job{ID: "j1", Payload: json.RawMessage(`{"connector_id":"c1"}`)})
	require.NoError(t, err)
	res := out.Result.(KnowledgeResult)

	assert.Equal(t, "c0", src.seen["ENG"], "the stored cursor is passed to the source")
	assert.Equal(t, "c2", ps.cursors["ENG"], "the cursor advances page by page")
	assert.Equal(t, 4, res.Docs, "2 changed + 2 labelled documents stored")
	assert.Equal(t, 3, embedded, "one embed call per applied batch (2 pages + the labelled set)")
	assert.True(t, res.Reconcile)
	assert.Equal(t, []string{"ENG-1", "ENG-2"}, docs.removed["ENG"])
	assert.Equal(t, "2026-10-02T12:00:00Z", ps.cursors[reconcileStream])

	assert.Equal(t, "label_only", rules.rules["ENG-1"].Action, "Done flips a suppressing rule")
	assert.Equal(t, NoteFixedUpstream, rules.rules["ENG-1"].UpstreamNote)
	assert.Equal(t, "label_only", rules.rules["ENG-9"].Action, "label removed flips a label-managed rule")
	assert.Equal(t, NoteLabelRemoved, rules.rules["ENG-9"].UpstreamNote)
	assert.Equal(t, "suppress", rules.rules["ENG-8"].Action, "a human's later choice is kept")

	require.Len(t, rules.created, 2)
	byRef := map[string]ports.ImportedRule{}
	for _, r := range rules.created {
		byRef[r.Ref] = r
	}
	assert.Equal(t, "suppress", byRef["ENG-2"].Action)
	assert.Equal(t, "in_progress", byRef["ENG-2"].Reason)
	assert.JSONEq(t, `{"services":["checkout"]}`, string(byRef["ENG-2"].Match))
	assert.Equal(t, "label_only", byRef["ENG-3"].Action, "an issue already Done imports as label_only")
	assert.Equal(t, 2, res.Imported)
	assert.Equal(t, 2, res.Flipped)
	assert.Equal(t, 1, reloaded)
	assert.NoError(t, ps.health)

	// Second run: nothing new to import, reconcile not due, no flips, no reload.
	out, err = k.Handle(ctx, ports.Job{ID: "j2", Payload: json.RawMessage(`{"connector_id":"c1"}`)})
	require.NoError(t, err)
	res = out.Result.(KnowledgeResult)
	assert.Equal(t, 0, res.Imported)
	assert.Equal(t, 0, res.Flipped)
	assert.False(t, res.Reconcile)
	assert.Equal(t, 1, reloaded)
}

func TestKnowledgeSyncNotes(t *testing.T) {
	src := &fakeKnowledgeSource{labeled: []ports.KnowledgeDoc{doc("ENG-5", false, "known-issue")}}
	rules := &fakeRules{rules: map[string]*ports.UpstreamRule{}}
	ps := &fakePollStore{cc: ports.ConnectorConfig{ID: "c1", Type: "jira", Config: map[string]string{"known_issue_label": "Known-Issue"}}, cursors: map[string]string{}}
	k := &KnowledgeSync{Store: ps, Sources: map[string]ports.KnowledgeSource{"jira": src}, Docs: &fakeDocs{}, Rules: rules,
		Embed: func(context.Context, llmgateway.CallMeta, []ports.Chunk) (int, error) {
			return 0, &ports.SpendBlockedError{Scope: "global"}
		},
		Propose: func(context.Context, string) (Proposal, error) { return Proposal{}, llmgateway.ErrNoRoute }}
	out, err := k.Handle(context.Background(), ports.Job{Payload: json.RawMessage(`{"connector_id":"c1"}`)})
	require.NoError(t, err)
	res := out.Result.(KnowledgeResult)
	require.Len(t, rules.created, 1)
	assert.Empty(t, rules.created[0].Match, "no route: the draft has no match")
	assert.Len(t, res.Notes, 2)

	// A disabled label ("-") skips the import.
	ps.cc.Config["known_issue_label"] = "-"
	rules.created = nil
	_, err = k.Handle(context.Background(), ports.Job{Payload: json.RawMessage(`{"connector_id":"c1"}`)})
	require.NoError(t, err)
	assert.Empty(t, rules.created)
}

func TestKnowledgeSyncErrorsAndFetchLink(t *testing.T) {
	bad := &badSource{fakeKnowledgeSource{}}
	ps := &fakePollStore{cc: ports.ConnectorConfig{ID: "c1", Type: "jira"}, cursors: map[string]string{}}
	k := &KnowledgeSync{Store: ps, Sources: map[string]ports.KnowledgeSource{"jira": bad}, Docs: &fakeDocs{}}
	_, err := k.Handle(context.Background(), ports.Job{Payload: json.RawMessage(`{"connector_id":"c1"}`)})
	var perm *ports.PermanentError
	require.True(t, errors.As(err, &perm), "a misconfigured connector fails permanently: %v", err)
	assert.Error(t, ps.health)

	ok := &KnowledgeSync{Store: ps, Sources: map[string]ports.KnowledgeSource{"jira": &fakeKnowledgeSource{}}}
	d, err := ok.FetchLink(context.Background(), "https://acme.atlassian.net/browse/ENG-1")
	require.NoError(t, err)
	assert.Equal(t, "ENG-1", d.ExternalID)
	_, err = ok.FetchLink(context.Background(), "https://elsewhere.example.com/x")
	assert.ErrorIs(t, err, ErrNoKnowledgeConnector)
}

type badSource struct{ fakeKnowledgeSource }

func (b *badSource) Streams(ports.ConnectorConfig) ([]string, error) {
	return nil, &ports.ValidationError{Code: "INVALID_CONNECTOR", Message: "jira: projects is required"}
}
