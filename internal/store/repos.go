package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Sealer encrypts secrets (secrets.Box).
type Sealer interface {
	Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	Opener
}

// Connectors stores connectors with envelope-encrypted credentials and webhook secrets.
type Connectors struct {
	s   *Store
	box Sealer
	// CredsAAD and WebhookAAD bind ciphertexts to their row (secrets.ConnectorCredsAAD / ConnectorWebhookAAD).
	CredsAAD, WebhookAAD func(id string) []byte
}

// NewConnectors returns the connector store.
func NewConnectors(s *Store, box Sealer, credsAAD, webhookAAD func(string) []byte) *Connectors {
	return &Connectors{s: s, box: box, CredsAAD: credsAAD, WebhookAAD: webhookAAD}
}

// NewConnector is a connector to create.
type NewConnector struct {
	Type, Name, Mode string
	PollSeconds      int64
	Config           map[string]string
	Credentials      string
	WebhookSecret    string
}

// Create stores a connector, sealing its secrets.
func (c *Connectors) Create(ctx context.Context, n NewConnector) (string, error) {
	id := ports.NewID()
	cfg, _ := json.Marshal(orEmpty(n.Config))
	p := gen.CreateConnectorParams{ID: id, Type: gen.ConnectorType(n.Type), Name: n.Name, Config: cfg,
		Mode: gen.ConnectorMode(defaultStr(n.Mode, "webhook")), PollSeconds: float64(defaultInt(n.PollSeconds, 60))}
	if n.Credentials != "" {
		ct, err := c.box.Seal(ctx, []byte(n.Credentials), c.CredsAAD(id))
		if err != nil {
			return "", err
		}
		p.CredsCiphertext = ct
	}
	if n.WebhookSecret != "" {
		ct, err := c.box.Seal(ctx, []byte(n.WebhookSecret), c.WebhookAAD(id))
		if err != nil {
			return "", err
		}
		h := sha256.Sum256([]byte(n.WebhookSecret))
		p.WebhookSecretCt, p.WebhookSecretHash = ct, h[:]
	}
	if _, err := c.s.Q.CreateConnector(ctx, p); err != nil {
		return "", fmt.Errorf("create connector: %w", err)
	}
	return id, nil
}

// Get loads a connector and decrypts its secrets. Missing → ports.ErrNotFound.
func (c *Connectors) Get(ctx context.Context, id string) (ports.ConnectorConfig, error) {
	if !uuidRE.MatchString(id) { // e.g. a mistyped webhook URL: unknown, not a database error
		return ports.ConnectorConfig{}, fmt.Errorf("connector %q: %w", id, ports.ErrNotFound)
	}
	r, err := c.s.Q.GetConnector(ctx, id)
	if IsNoRows(err) {
		return ports.ConnectorConfig{}, fmt.Errorf("connector %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return ports.ConnectorConfig{}, fmt.Errorf("load connector: %w", err)
	}
	if !r.Enabled {
		return ports.ConnectorConfig{}, fmt.Errorf("connector %s is disabled: %w", id, ports.ErrNotFound)
	}
	return c.decode(ctx, gen.ListConnectorsByTypeRow(r))
}

// ListByType lists enabled connectors of the given types, decrypted.
func (c *Connectors) ListByType(ctx context.Context, types ...string) ([]ports.ConnectorConfig, error) {
	rows, err := c.s.Q.ListConnectorsByType(ctx, types)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	out := make([]ports.ConnectorConfig, 0, len(rows))
	for _, r := range rows {
		cc, err := c.decode(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, cc)
	}
	return out, nil
}

// SetHealth records a sync outcome.
func (c *Connectors) SetHealth(ctx context.Context, id string, syncErr error) error {
	h, msg := gen.HealthStateOk, ""
	if syncErr != nil {
		h, msg = gen.HealthStateFailing, truncate(syncErr.Error(), 1000)
	}
	return c.s.Q.SetConnectorHealth(ctx, gen.SetConnectorHealthParams{ID: id, Health: h, LastError: msg, Synced: syncErr == nil})
}

func (c *Connectors) decode(ctx context.Context, r gen.ListConnectorsByTypeRow) (ports.ConnectorConfig, error) {
	cc := ports.ConnectorConfig{ID: r.ID, Type: string(r.Type), Name: r.Name, Mode: string(r.Mode), PollSeconds: r.PollSeconds,
		Config: map[string]string{}}
	if len(r.Config) > 0 {
		if err := json.Unmarshal(r.Config, &cc.Config); err != nil {
			return cc, fmt.Errorf("connector %s config: %w", r.Name, err)
		}
	}
	if len(r.CredsCiphertext) > 0 {
		b, err := c.box.Open(ctx, r.CredsCiphertext, c.CredsAAD(r.ID))
		if err != nil {
			return cc, ports.Permanent(fmt.Errorf("decrypt credentials for connector %q: %w", r.Name, err))
		}
		cc.Credentials = string(b)
	}
	if len(r.WebhookSecretCt) > 0 {
		b, err := c.box.Open(ctx, r.WebhookSecretCt, c.WebhookAAD(r.ID))
		if err != nil {
			return cc, ports.Permanent(fmt.Errorf("decrypt webhook secret for connector %q: %w", r.Name, err))
		}
		cc.WebhookSecret = string(b)
	}
	return cc, nil
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Repos stores tracked repositories.
type Repos struct{ s *Store }

// NewRepos returns the repo store.
func NewRepos(s *Store) *Repos { return &Repos{s: s} }

// Upsert creates or updates a repository by (connector, full name) and returns its ID.
func (r *Repos) Upsert(ctx context.Context, rc ports.RepoConfig) (string, error) {
	p := rc.Push
	id, err := r.s.Q.UpsertRepo(ctx, gen.UpsertRepoParams{
		ID: ports.NewID(), ConnectorID: rc.ConnectorID, FullName: rc.FullName, DefaultBranch: defaultStr(rc.DefaultBranch, "main"),
		TrackedBranch: rc.TrackedBranch, DocsPath: defaultStr(rc.DocsPath, "docs/generated/"),
		PushMode: gen.PushMode(defaultStr(string(p.Mode), string(ports.PushPRAutoMerge))),
		OnReject: defaultStr(p.OnReject, ports.RejectFallbackAutoMerge), Approver: p.Approver, ServiceName: rc.ServiceName,
		PrConflictStrategy: defaultStr(p.ConflictStrategy, ports.ConflictAutoRebase),
		StaleSeconds:       defaultDur(p.StaleAfter, 72*time.Hour).Seconds(),
	})
	if err != nil {
		return "", fmt.Errorf("upsert repo %s: %w", rc.FullName, err)
	}
	return id, nil
}

// Get loads a repository. Missing → ports.ErrNotFound.
func (r *Repos) Get(ctx context.Context, id string) (ports.RepoConfig, error) {
	row, err := r.s.Q.GetRepo(ctx, id)
	if IsNoRows(err) {
		return ports.RepoConfig{}, fmt.Errorf("repo %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return ports.RepoConfig{}, fmt.Errorf("load repo: %w", err)
	}
	return repoConfig(gen.ListEnabledReposRow(row)), nil
}

// ByName finds a connector's repository by full name. Missing → ports.ErrNotFound.
func (r *Repos) ByName(ctx context.Context, connectorID, fullName string) (ports.RepoConfig, error) {
	row, err := r.s.Q.GetRepoByName(ctx, gen.GetRepoByNameParams{ConnectorID: connectorID, FullName: fullName})
	if IsNoRows(err) {
		return ports.RepoConfig{}, fmt.Errorf("repo %s: %w", fullName, ports.ErrNotFound)
	}
	if err != nil {
		return ports.RepoConfig{}, fmt.Errorf("load repo: %w", err)
	}
	return repoConfig(gen.ListEnabledReposRow(row)), nil
}

// ListEnabled lists enabled repositories of enabled connectors.
func (r *Repos) ListEnabled(ctx context.Context) ([]ports.RepoConfig, error) {
	rows, err := r.s.Q.ListEnabledRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	out := make([]ports.RepoConfig, len(rows))
	for i, row := range rows {
		out[i] = repoConfig(row)
	}
	return out, nil
}

// SetLastProcessed records the last commit whose docs are current.
func (r *Repos) SetLastProcessed(ctx context.Context, id, sha string) error {
	return r.s.Q.SetLastProcessedSHA(ctx, gen.SetLastProcessedSHAParams{ID: id, Sha: sha})
}

func repoConfig(r gen.ListEnabledReposRow) ports.RepoConfig {
	return ports.RepoConfig{ID: r.ID, ConnectorID: r.ConnectorID, ConnectorType: string(r.ConnectorType), FullName: r.FullName,
		DefaultBranch: r.DefaultBranch, TrackedBranch: r.TrackedBranch, DocsPath: r.DocsPath, LastProcessedSHA: r.LastProcessedSha,
		ServiceName: r.ServiceName, Enabled: r.Enabled, Push: ports.PushConfig{Mode: ports.PushMode(r.PushMode), OnReject: r.OnReject,
			Approver: r.Approver, ConflictStrategy: r.PrConflictStrategy, StaleAfter: time.Duration(r.StaleSeconds) * time.Second}}
}

// PRs implements ports.PRStore.
type PRs struct{ s *Store }

// NewPRs returns the docs-PR store.
func NewPRs(s *Store) *PRs { return &PRs{s: s} }

// SavePR inserts or updates a PR record.
func (p *PRs) SavePR(ctx context.Context, r ports.PRRecord) error {
	opened := r.OpenedAt
	if opened.IsZero() {
		opened = time.Now()
	}
	return p.s.Q.SavePR(ctx, gen.SavePRParams{RepoID: r.RepoID, Number: int32(r.Number), Url: r.URL, Branch: r.Branch, Base: r.Base,
		JobID: strPtr(r.JobID), Mode: gen.PushMode(r.Mode), State: r.State, ChunkIds: nonNilSlice(r.ChunkIDs),
		SourcePaths: nonNilSlice(r.SourcePaths), Approver: r.Approver, Note: r.Note, OpenedAt: opened})
}

// GetPR loads one record. Missing → ports.ErrNotFound.
func (p *PRs) GetPR(ctx context.Context, repoID string, number int) (ports.PRRecord, error) {
	r, err := p.s.Q.GetPRRecord(ctx, gen.GetPRRecordParams{RepoID: repoID, Number: int32(number)})
	if IsNoRows(err) {
		return ports.PRRecord{}, fmt.Errorf("pr %d: %w", number, ports.ErrNotFound)
	}
	if err != nil {
		return ports.PRRecord{}, err
	}
	return prRecord(r), nil
}

// ActivePRs lists a repo's open/awaiting-review docs PRs.
func (p *PRs) ActivePRs(ctx context.Context, repoID string) ([]ports.PRRecord, error) {
	rows, err := p.s.Q.ActivePRs(ctx, repoID)
	return prRecords(rows, err)
}

// AllActivePRs lists every managed docs PR.
func (p *PRs) AllActivePRs(ctx context.Context) ([]ports.PRRecord, error) {
	rows, err := p.s.Q.AllActivePRs(ctx)
	return prRecords(rows, err)
}

// SetPRState moves a PR record to a new state.
func (p *PRs) SetPRState(ctx context.Context, repoID string, number int, state, note string) error {
	n, err := p.s.Q.SetPRState(ctx, gen.SetPRStateParams{RepoID: repoID, Number: int32(number), State: state, Note: note})
	if err == nil && n == 0 {
		return fmt.Errorf("pr %d: %w", number, ports.ErrNotFound)
	}
	return err
}

func prRecords(rows []gen.Pr, err error) ([]ports.PRRecord, error) {
	if err != nil {
		return nil, fmt.Errorf("list prs: %w", err)
	}
	out := make([]ports.PRRecord, len(rows))
	for i, r := range rows {
		out[i] = prRecord(r)
	}
	return out, nil
}

func prRecord(r gen.Pr) ports.PRRecord {
	rec := ports.PRRecord{RepoID: r.RepoID, Number: int(r.Number), URL: r.Url, Branch: r.Branch, Base: r.Base, Mode: ports.PushMode(r.Mode),
		State: r.State, ChunkIDs: r.ChunkIds, SourcePaths: r.SourcePaths, Approver: r.Approver, Note: r.Note, OpenedAt: r.OpenedAt, UpdatedAt: r.UpdatedAt}
	if r.JobID != nil {
		rec.JobID = *r.JobID
	}
	return rec
}

// Savings records usage avoided (analytics "saved" figures).
type Savings struct{ s *Store }

// NewSavings returns the savings recorder.
func NewSavings(s *Store) *Savings { return &Savings{s: s} }

// Record inserts one savings event.
func (v *Savings) Record(ctx context.Context, kind string, tokens int64, costUSD float64, ref string) error {
	return v.s.Q.InsertSavings(ctx, gen.InsertSavingsParams{ID: ports.NewID(), Kind: gen.SavingsKind(kind), EstTokensAvoided: tokens,
		EstCostAvoidedUsd: costUSD, RefID: ref})
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func defaultInt(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}

func defaultDur(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var _ ports.PRStore = (*PRs)(nil)

// ListAll lists every repository (enabled or not).
func (r *Repos) ListAll(ctx context.Context) ([]ports.RepoConfig, error) {
	rows, err := r.s.Q.ListAllRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	out := make([]ports.RepoConfig, len(rows))
	for i, row := range rows {
		out[i] = repoConfig(gen.ListEnabledReposRow(row))
	}
	return out, nil
}
