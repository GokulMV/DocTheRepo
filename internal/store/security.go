package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/security"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Security stores security scans and findings.
type Security struct{ s *Store }

// NewSecurity returns the security store.
func NewSecurity(s *Store) *Security { return &Security{s: s} }

// Scan is a security scan with, when read on its own, its findings.
type Scan struct {
	ID         string             `json:"id"`
	RepoID     string             `json:"repo_id"`
	RepoName   string             `json:"repo"`
	JobID      string             `json:"job_id,omitempty"`
	Status     string             `json:"status"`
	CommitSHA  string             `json:"commit_sha"`
	Modules    []string           `json:"modules"`
	Verdict    string             `json:"verdict"`
	Summary    json.RawMessage    `json:"summary"`
	Error      string             `json:"error,omitempty"`
	StartedBy  string             `json:"started_by,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	Findings   []security.Finding `json:"findings,omitempty"`
}

// CreateScan records a queued scan.
func (x *Security) CreateScan(ctx context.Context, repoID string, modules []string, userID string) (string, error) {
	id := ports.NewID()
	_, err := x.s.Pool.Exec(ctx, `INSERT INTO security_scans (id, repo_id, modules, started_by) VALUES ($1, $2, $3, $4)`, id, repoID, modules, strPtr(userID))
	return id, err
}

// SetScanJob links the scan to its job.
func (x *Security) SetScanJob(ctx context.Context, scanID, jobID string) error {
	_, err := x.s.Pool.Exec(ctx, `UPDATE security_scans SET job_id = $2 WHERE id = $1`, scanID, jobID)
	return err
}

// StartScan implements security.Store.
func (x *Security) StartScan(ctx context.Context, scanID, commit string) error {
	_, err := x.s.Pool.Exec(ctx, `UPDATE security_scans SET status = 'running', commit_sha = $2 WHERE id = $1`, scanID, commit)
	return err
}

// FinishScan implements security.Store.
func (x *Security) FinishScan(ctx context.Context, scanID, status, verdict string, summary any, errMsg string) error {
	b, _ := json.Marshal(summary)
	if summary == nil {
		b = []byte("{}")
	}
	_, err := x.s.Pool.Exec(ctx, `UPDATE security_scans SET status = $2, verdict = $3, summary = $4, error = $5, finished_at = now() WHERE id = $1`,
		scanID, status, verdict, b, errMsg)
	return err
}

// AddFindings implements security.Store.
func (x *Security) AddFindings(ctx context.Context, scanID, repoID string, fs []security.Finding) error {
	return x.s.InTx(ctx, func(_ *gen.Queries, tx pgx.Tx) error {
		for _, f := range fs {
			repro, _ := json.Marshal(nonNilStrings(f.Repro))
			if _, err := tx.Exec(ctx, `INSERT INTO security_findings (id, scan_id, repo_id, dimension, title, surface, file, line_start, line_end,
					repro, evidence, severity, exploitability, priority, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
				ports.NewID(), scanID, repoID, f.Dimension, f.Title, f.Surface, f.File, f.LineStart, f.LineEnd, repro, f.Evidence,
				f.Severity, f.Exploitability, f.Priority, f.Status); err != nil {
				return err
			}
		}
		return nil
	})
}

const findingCols = `id, dimension, title, surface, file, line_start, line_end, repro, evidence, severity, exploitability, priority, status,
	fix_status, fix, fix_pr_url, fix_error`

func scanFinding(row pgx.Row) (security.Finding, error) {
	var f security.Finding
	var repro, fix []byte
	err := row.Scan(&f.ID, &f.Dimension, &f.Title, &f.Surface, &f.File, &f.LineStart, &f.LineEnd, &repro, &f.Evidence, &f.Severity,
		&f.Exploitability, &f.Priority, &f.Status, &f.FixStatus, &fix, &f.FixPRURL, &f.FixError)
	if err != nil {
		return f, err
	}
	_ = json.Unmarshal(repro, &f.Repro)
	if len(fix) > 2 {
		var fx security.Fix
		if json.Unmarshal(fix, &fx) == nil {
			f.Fix = &fx
		}
	}
	return f, nil
}

func (x *Security) findings(ctx context.Context, where string, args ...any) ([]security.Finding, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT `+findingCols+` FROM security_findings WHERE `+where+
		` ORDER BY priority, CASE status WHEN 'confirmed' THEN 0 WHEN 'plausible' THEN 1 ELSE 2 END, file, line_start`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []security.Finding{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Findings implements security.Store: the given findings of one repository.
func (x *Security) Findings(ctx context.Context, repoID string, ids []string) ([]security.Finding, error) {
	return x.findings(ctx, `repo_id = $1 AND id = ANY($2::uuid[])`, repoID, ids)
}

// SetFix implements security.Store.
func (x *Security) SetFix(ctx context.Context, id, status string, fix *security.Fix, prURL, errMsg string) error {
	b := []byte("{}")
	if fix != nil {
		b, _ = json.Marshal(fix)
	}
	_, err := x.s.Pool.Exec(ctx, `UPDATE security_findings SET fix_status = $2, fix = CASE WHEN $3::jsonb = '{}'::jsonb THEN fix ELSE $3::jsonb END,
		fix_pr_url = CASE WHEN $4 = '' THEN fix_pr_url ELSE $4 END, fix_error = $5 WHERE id = $1`, id, status, b, prURL, errMsg)
	return err
}

// QueueFixes marks the findings to fix and returns the ones that can be: verified (not rejected) and not
// already queued or in a pull request.
func (x *Security) QueueFixes(ctx context.Context, repoID string, ids []string) ([]string, error) {
	rows, err := x.s.Pool.Query(ctx, `UPDATE security_findings SET fix_status = 'queued', fix_error = ''
		WHERE repo_id = $1 AND id = ANY($2::uuid[]) AND status <> 'rejected' AND fix_status NOT IN ('queued', 'pr_opened')
		RETURNING id`, repoID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ModuleFindings implements security.Cache.
func (x *Security) ModuleFindings(ctx context.Context, repoID, module, hash string) ([]security.Finding, bool, error) {
	var b []byte
	err := x.s.Pool.QueryRow(ctx, `SELECT findings FROM security_module_cache WHERE repo_id = $1 AND module = $2 AND inputs_hash = $3`,
		repoID, module, hash).Scan(&b)
	if IsNoRows(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var fs []security.Finding
	err = json.Unmarshal(b, &fs)
	return fs, err == nil, err
}

// PutModuleFindings implements security.Cache.
func (x *Security) PutModuleFindings(ctx context.Context, repoID, module, hash string, fs []security.Finding) error {
	if fs == nil {
		fs = []security.Finding{}
	}
	b, _ := json.Marshal(fs)
	_, err := x.s.Pool.Exec(ctx, `INSERT INTO security_module_cache (repo_id, module, inputs_hash, findings) VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_id, module, inputs_hash) DO UPDATE SET findings = EXCLUDED.findings, created_at = now()`, repoID, module, hash, b)
	return err
}

const scanCols = `s.id, s.repo_id, r.full_name, coalesce(s.job_id::text, ''), s.status, s.commit_sha, s.modules, s.verdict, s.summary, s.error,
	coalesce(u.email, ''), s.created_at, s.finished_at`

func scanScan(row pgx.Row) (Scan, error) {
	var sc Scan
	err := row.Scan(&sc.ID, &sc.RepoID, &sc.RepoName, &sc.JobID, &sc.Status, &sc.CommitSHA, &sc.Modules, &sc.Verdict, &sc.Summary, &sc.Error,
		&sc.StartedBy, &sc.CreatedAt, &sc.FinishedAt)
	return sc, err
}

// GetScan reads one scan with its findings.
func (x *Security) GetScan(ctx context.Context, id string) (Scan, error) {
	sc, err := scanScan(x.s.Pool.QueryRow(ctx, `SELECT `+scanCols+` FROM security_scans s JOIN repos r ON r.id = s.repo_id
		LEFT JOIN users u ON u.id = s.started_by WHERE s.id = $1`, id))
	if IsNoRows(err) {
		return sc, ports.ErrNotFound
	}
	if err != nil {
		return sc, err
	}
	sc.Findings, err = x.findings(ctx, `scan_id = $1`, id)
	return sc, err
}

// LatestScans returns each readable repository's most recent scan (all: every repository).
func (x *Security) LatestScans(ctx context.Context, all bool, repoIDs []string) ([]Scan, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT DISTINCT ON (s.repo_id) `+scanCols+` FROM security_scans s JOIN repos r ON r.id = s.repo_id
		LEFT JOIN users u ON u.id = s.started_by WHERE $1 OR s.repo_id = ANY($2::uuid[]) ORDER BY s.repo_id, s.created_at DESC`, all, nonNilStrings(repoIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		sc, err := scanScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// RepoScans lists a repository's scans, newest first.
func (x *Security) RepoScans(ctx context.Context, repoID string, limit int) ([]Scan, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT `+scanCols+` FROM security_scans s JOIN repos r ON r.id = s.repo_id
		LEFT JOIN users u ON u.id = s.started_by WHERE s.repo_id = $1 ORDER BY s.created_at DESC LIMIT $2`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		sc, err := scanScan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func nonNilStrings(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
