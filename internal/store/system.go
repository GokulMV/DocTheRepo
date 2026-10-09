package store

import (
	"context"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// SystemLinks lists how the tracked repositories talk to each other: events one publishes and another
// consumes, calls into another repository's code or endpoints, and packages built from another tracked
// repository.
func (r *RepoDocs) SystemLinks(ctx context.Context) ([]repodocs.SystemLink, error) {
	names := map[string]string{}
	rows, err := r.s.Pool.Query(ctx, `SELECT id::text, full_name FROM repos WHERE enabled`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = n
	}
	rows.Close()
	type key struct{ from, to, kind, via string }
	acc := map[key]*repodocs.SystemLink{}
	add := func(from, to, kind, via, path string, line int) {
		if from == to || names[from] == "" || names[to] == "" {
			return
		}
		k := key{from, to, kind, via}
		if l, ok := acc[k]; ok {
			l.N++
			return
		}
		acc[k] = &repodocs.SystemLink{FromRepo: from, FromName: names[from], ToRepo: to, ToName: names[to], Kind: kind, Via: via, Path: path, Line: line, N: 1}
	}

	// Events: one repository publishes a topic another subscribes to.
	rows, err = r.s.Pool.Query(ctx, `
		SELECT p.repo_id::text, s.repo_id::text, t.name, coalesce(p.evidence->>'path', p.source_path), coalesce((p.evidence->>'line')::int, 0)
		FROM edges p JOIN entities t ON t.id = p.dst_id AND t.kind = 'queue_topic' AND t.deleted_at IS NULL
		JOIN edges s ON s.dst_id = t.id AND s.kind = 'subscribes' AND s.deleted_at IS NULL
		WHERE p.kind = 'publishes' AND p.deleted_at IS NULL AND p.repo_id IS NOT NULL AND s.repo_id IS NOT NULL AND p.repo_id <> s.repo_id
		LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, to, via, path string
		var line int
		if err := rows.Scan(&from, &to, &via, &path, &line); err != nil {
			rows.Close()
			return nil, err
		}
		add(from, to, "event", via, path, line)
	}
	rows.Close()

	// Code in one repository pointing at another's: calls, endpoints, services.
	rows, err = r.s.Pool.Query(ctx, `
		SELECT x.repo_id::text, d.repo_id::text, x.kind, d.kind, d.name, coalesce(x.evidence->>'path', x.source_path), coalesce((x.evidence->>'line')::int, 0)
		FROM edges x JOIN entities d ON d.id = x.dst_id AND d.deleted_at IS NULL
		WHERE x.deleted_at IS NULL AND x.repo_id IS NOT NULL AND d.repo_id IS NOT NULL AND x.repo_id <> d.repo_id
		  AND x.kind NOT IN ('contains', 'publishes', 'subscribes', 'documented_in', 'runbook_for', 'owned_by')
		LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, to, edge, dkind, via, path string
		var line int
		if err := rows.Scan(&from, &to, &edge, &dkind, &via, &path, &line); err != nil {
			rows.Close()
			return nil, err
		}
		kind := "other"
		switch {
		case dkind == "endpoint":
			kind = "api"
		case edge == "calls":
			kind = "call"
		case edge == "depends_on":
			kind = "library"
		}
		add(from, to, kind, via, path, line)
	}
	rows.Close()

	// Packages: a dependency whose name is another tracked repository (github.com/acme/lib, @acme/lib).
	rows, err = r.s.Pool.Query(ctx, `
		SELECT x.repo_id::text, d.name, coalesce(x.evidence->>'path', x.source_path), coalesce((x.evidence->>'line')::int, 0)
		FROM edges x JOIN entities d ON d.id = x.dst_id AND d.kind = 'dependency' AND d.deleted_at IS NULL
		WHERE x.kind = 'depends_on' AND x.deleted_at IS NULL AND x.repo_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, dep, path string
		var line int
		if err := rows.Scan(&from, &dep, &path, &line); err != nil {
			rows.Close()
			return nil, err
		}
		l := strings.ToLower(dep)
		for id, full := range names {
			f := strings.ToLower(full)
			if l == f || l == "@"+f || strings.HasSuffix(l, "/"+f) || strings.Contains(l, "/"+f+"/") {
				add(from, id, "library", dep, path, line)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Configuration and pipelines: hosts called, images run, repositories referenced in CI.
	wired, err := r.wiringRepos(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range repodocs.MatchWiring(wired) {
		add(l.FromRepo, l.ToRepo, l.Kind, l.Via, l.Path, l.Line)
	}
	out := make([]repodocs.SystemLink, 0, len(acc))
	for _, l := range acc {
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.FromName != b.FromName {
			return a.FromName < b.FromName
		}
		if a.ToName != b.ToName {
			return a.ToName < b.ToName
		}
		return a.Kind+a.Via < b.Kind+b.Via
	})
	return out, nil
}

// SystemDoc returns the system-wide document (ports.ErrNotFound when none).
func (r *RepoDocs) SystemDoc(ctx context.Context) (repodocs.Doc, error) {
	return scanDoc(r.s.Pool.QueryRow(ctx, `SELECT `+docCols+` FROM repo_docs WHERE repo_id IS NULL AND doc_type = 'system' AND doc_key = 'system'`))
}

// DeleteSystemDoc removes it (the repositories no longer talk to each other).
func (r *RepoDocs) DeleteSystemDoc(ctx context.Context) error {
	_, err := r.s.Pool.Exec(ctx, `DELETE FROM repo_docs WHERE repo_id IS NULL AND doc_type = 'system'`)
	return err
}

// SystemChunks returns the System architecture's stored search pieces (live and removed), wherever they sit.
func (r *RepoDocs) SystemChunks(ctx context.Context) ([]ports.Chunk, error) {
	rows, err := r.s.Q.ChunksAtPathAnyRepo(ctx, repodocs.SystemPath)
	if err != nil {
		return nil, err
	}
	return toChunks(rows), nil
}

// wiringRepos loads every enabled repository with its wiring facts.
func (r *RepoDocs) wiringRepos(ctx context.Context) ([]repodocs.WiringRepo, error) {
	rows, err := r.s.Pool.Query(ctx, `SELECT r.id::text, r.full_name, coalesce(r.service_name, ''), w.kind, w.value, w.note, w.path, w.line
		FROM repos r LEFT JOIN repo_wiring w ON w.repo_id = r.id WHERE r.enabled ORDER BY r.full_name, w.path, w.line`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repodocs.WiringRepo
	at := map[string]int{}
	for rows.Next() {
		var id, name, svc string
		var kind, value, note, path *string
		var line *int
		if err := rows.Scan(&id, &name, &svc, &kind, &value, &note, &path, &line); err != nil {
			return nil, err
		}
		i, ok := at[id]
		if !ok {
			i = len(out)
			at[id] = i
			out = append(out, repodocs.WiringRepo{ID: id, Name: name, Service: svc})
		}
		if kind != nil {
			out[i].Wires = append(out[i].Wires, repodocs.Wire{Kind: *kind, Value: *value, Note: *note, Path: *path, Line: *line})
		}
	}
	return out, rows.Err()
}

// PutWiring replaces a repository's wiring facts; it reports whether they changed.
func (r *RepoDocs) PutWiring(ctx context.Context, repoID string, ws []repodocs.Wire) (bool, error) {
	fp := func(ws []repodocs.Wire) string {
		parts := make([]string, 0, len(ws))
		for _, w := range ws {
			parts = append(parts, w.Kind+"\x00"+w.Value+"\x00"+w.Path)
		}
		sort.Strings(parts)
		return strings.Join(parts, "\n")
	}
	tx, err := r.s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT kind, value, path FROM repo_wiring WHERE repo_id = $1`, repoID)
	if err != nil {
		return false, err
	}
	var prev []repodocs.Wire
	for rows.Next() {
		var w repodocs.Wire
		if err := rows.Scan(&w.Kind, &w.Value, &w.Path); err != nil {
			rows.Close()
			return false, err
		}
		prev = append(prev, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	if fp(prev) == fp(ws) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM repo_wiring WHERE repo_id = $1`, repoID); err != nil {
		return false, err
	}
	for _, w := range ws {
		if _, err := tx.Exec(ctx, `INSERT INTO repo_wiring (repo_id, kind, value, note, path, line) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`, repoID, w.Kind, w.Value, w.Note, w.Path, w.Line); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
