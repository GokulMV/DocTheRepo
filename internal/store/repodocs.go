package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/repodocs"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// RepoDocs stores Docs v2 documents and file cards, and reads the facts they are written from.
type RepoDocs struct{ s *Store }

// NewRepoDocs returns the store.
func NewRepoDocs(s *Store) *RepoDocs { return &RepoDocs{s: s} }

// Facts gathers what the index and the knowledge graph know about a repository (no model).
func (r *RepoDocs) Facts(ctx context.Context, repoID string) (*repodocs.Facts, error) {
	f := &repodocs.Facts{RepoID: repoID, Special: map[string]string{}}
	if err := r.s.Pool.QueryRow(ctx, `SELECT full_name, coalesce(service_name, ''), coalesce(last_processed_sha, '') FROM repos WHERE id = $1`, repoID).
		Scan(&f.Repo, &f.Service, &f.Head); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}
	type symKey struct{ path, name string }
	lines := map[symKey]int{}
	meta := map[symKey]repodocs.Symbol{}
	rows, err := r.s.Pool.Query(ctx, `
		SELECT coalesce(e.attrs->>'path', ''), e.name, coalesce(e.attrs->>'kind', ''), coalesce(e.attrs->>'signature', ''),
		       coalesce((ed.evidence->>'line')::int, 0)
		FROM edges ed JOIN entities e ON e.id = ed.dst_id
		WHERE ed.repo_id = $1 AND ed.kind = 'contains' AND e.kind = 'symbol' AND ed.deleted_at IS NULL AND e.deleted_at IS NULL`, repoID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s repodocs.Symbol
		if err := rows.Scan(&s.Path, &s.Name, &s.Kind, &s.Signature, &s.Line); err != nil {
			rows.Close()
			return nil, err
		}
		k := symKey{s.Path, s.Name}
		meta[k], lines[k] = s, s.Line
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type fileAcc struct {
		lang   string
		hashes []string
		syms   []repodocs.Symbol
		lines  int
		end    int
	}
	files := map[string]*fileAcc{}
	rows, err = r.s.Pool.Query(ctx, `SELECT path, symbol, language, content, content_hash FROM chunks
		WHERE repo_id = $1 AND source = 'code' AND deleted_at IS NULL ORDER BY path, symbol`, repoID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p, sym, lang, content, hash string
		if err := rows.Scan(&p, &sym, &lang, &content, &hash); err != nil {
			rows.Close()
			return nil, err
		}
		fa := files[p]
		if fa == nil {
			fa = &fileAcc{lang: lang}
			files[p] = fa
		}
		n := strings.Count(content, "\n") + 1
		fa.hashes = append(fa.hashes, hash)
		fa.lines += n
		if sym == "__module__" {
			continue // top-level code: counted, but it has no single line to cite
		}
		s := meta[symKey{p, sym}]
		s.Name, s.Path, s.Body, s.Lines = sym, p, content, n
		if s.Line > 0 {
			fa.end = max(fa.end, s.Line+n-1)
		}
		fa.syms = append(fa.syms, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Calls: file to file, and how often each symbol is called from other files.
	in := map[symKey]int{}
	calls := map[[2]string]int{}
	rows, err = r.s.Pool.Query(ctx, `
		SELECT coalesce(s.attrs->>'path', split_part(s.key, ':', 2)), coalesce(d.attrs->>'path', split_part(d.key, ':', 2)), d.name
		FROM edges ed JOIN entities s ON s.id = ed.src_id JOIN entities d ON d.id = ed.dst_id
		WHERE ed.repo_id = $1 AND ed.kind = 'calls' AND ed.deleted_at IS NULL`, repoID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var from, to, name string
		if err := rows.Scan(&from, &to, &name); err != nil {
			rows.Close()
			return nil, err
		}
		if from != to && from != "" && to != "" {
			calls[[2]string{from, to}]++
			in[symKey{to, name}]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for k, n := range calls {
		f.Calls = append(f.Calls, repodocs.Call{From: k[0], To: k[1], N: n})
	}
	sort.Slice(f.Calls, func(i, j int) bool { return f.Calls[i].From+f.Calls[i].To < f.Calls[j].From+f.Calls[j].To })

	for _, p := range sortedMapKeys(files) {
		fa := files[p]
		sort.SliceStable(fa.syms, func(i, j int) bool { return fa.syms[i].Line < fa.syms[j].Line })
		for i := range fa.syms {
			fa.syms[i].In = in[symKey{p, fa.syms[i].Name}]
		}
		sort.Strings(fa.hashes)
		f.Files = append(f.Files, repodocs.File{Path: p, Language: fa.lang, Lines: max(fa.lines, fa.end), Symbols: fa.syms,
			Hash: repodocs.Hash(fa.hashes...), Shape: repodocs.Shape(fa.syms)})
	}

	// Facts: what the code declares, with where.
	rows, err = r.s.Pool.Query(ctx, `
		SELECT ed.kind, d.kind, d.name, coalesce(ed.evidence->>'path', ed.source_path), coalesce((ed.evidence->>'line')::int, 0), s.kind, s.name
		FROM edges ed JOIN entities d ON d.id = ed.dst_id JOIN entities s ON s.id = ed.src_id
		WHERE ed.repo_id = $1 AND ed.deleted_at IS NULL AND d.deleted_at IS NULL
		  AND ed.kind IN ('exposes', 'reads_env', 'uses_datastore', 'publishes', 'subscribes', 'depends_on', 'owned_by', 'deployed_as')`, repoID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var edge, dkind, name, p, skind, sname string
		var line int
		if err := rows.Scan(&edge, &dkind, &name, &p, &line, &skind, &sname); err != nil {
			rows.Close()
			return nil, err
		}
		kind := map[string]string{"exposes": "endpoint", "reads_env": "env_var", "uses_datastore": "datastore", "publishes": "topic_pub",
			"subscribes": "topic_sub", "depends_on": "dependency", "owned_by": "owner", "deployed_as": "service"}[edge]
		if dkind == "cloud_resource" {
			kind = "cloud_resource"
		}
		if edge == "depends_on" && dkind != "dependency" {
			continue
		}
		from := ""
		if skind == "symbol" {
			from = sname
		}
		f.Facts = append(f.Facts, repodocs.Fact{Kind: kind, Name: name, Path: p, Line: line, From: from})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.s.Pool.Query(ctx, `SELECT path FROM architecture_diagrams WHERE repo_id = $1 ORDER BY path`, repoID)
	if err == nil {
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				f.Diagrams = append(f.Diagrams, p)
			}
		}
		rows.Close()
	}
	return f, nil
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Cards returns the repository's file cards by path.
func (r *RepoDocs) Cards(ctx context.Context, repoID string) (map[string]repodocs.Card, error) {
	rows, err := r.s.Pool.Query(ctx, `SELECT card FROM file_cards WHERE repo_id = $1`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]repodocs.Card{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var c repodocs.Card
		if json.Unmarshal(b, &c) == nil {
			out[c.Path] = c
		}
	}
	return out, rows.Err()
}

// PutCards stores cards.
func (r *RepoDocs) PutCards(ctx context.Context, repoID string, cards []repodocs.Card) error {
	b := &pgx.Batch{}
	for _, c := range cards {
		raw, _ := json.Marshal(c)
		b.Queue(`INSERT INTO file_cards (repo_id, path, shape, card, updated_at) VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (repo_id, path) DO UPDATE SET shape = EXCLUDED.shape, card = EXCLUDED.card, updated_at = now()`, repoID, c.Path, c.Shape, raw)
	}
	return r.s.Pool.SendBatch(ctx, b).Close()
}

const docCols = `id::text, coalesce(repo_id::text, ''), doc_type, doc_key, title, grp, ord, at_a_glance, sections, gaps, confidence, why, calibrated,
	inputs_hash, file_hashes, changed, source_sha, model, tokens_in, tokens_out, cost_usd, status, error, updated_at`

func scanDoc(row pgx.Row) (repodocs.Doc, error) {
	var d repodocs.Doc
	var sections, gaps, why, hashes []byte
	var conf, changed float32
	err := row.Scan(&d.ID, &d.RepoID, &d.Type, &d.Key, &d.Title, &d.Group, &d.Order, &d.AtAGlance, &sections, &gaps, &conf, &why, &d.Calibrated,
		&d.InputsHash, &hashes, &changed, &d.SourceSHA, &d.Model, &d.TokensIn, &d.TokensOut, &d.CostUSD, &d.Status, &d.Error, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ports.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Confidence, d.Changed = float64(conf), float64(changed)
	_ = json.Unmarshal(sections, &d.Sections)
	_ = json.Unmarshal(gaps, &d.Gaps)
	_ = json.Unmarshal(why, &d.Why)
	_ = json.Unmarshal(hashes, &d.FileHashes)
	return d, nil
}

// Docs returns every document of a repository, in navigation order.
func (r *RepoDocs) Docs(ctx context.Context, repoID string) ([]repodocs.Doc, error) {
	rows, err := r.s.Pool.Query(ctx, `SELECT `+docCols+` FROM repo_docs WHERE repo_id = $1 ORDER BY ord, title`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repodocs.Doc
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Doc returns one document.
func (r *RepoDocs) Doc(ctx context.Context, id string) (repodocs.Doc, error) {
	return scanDoc(r.s.Pool.QueryRow(ctx, `SELECT `+docCols+` FROM repo_docs WHERE id = $1`, id))
}

// PutDoc inserts or replaces a document by (repo, type, key).
func (r *RepoDocs) PutDoc(ctx context.Context, d repodocs.Doc) (repodocs.Doc, error) {
	if d.ID == "" {
		d.ID = ports.NewID()
	}
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = time.Now()
	}
	j := func(v any) []byte {
		b, _ := json.Marshal(v)
		if string(b) == "null" {
			return []byte("[]")
		}
		return b
	}
	conflict := "(repo_id, doc_type, doc_key)"
	var repoID any = d.RepoID
	if d.RepoID == "" {
		conflict, repoID = "(doc_type, doc_key) WHERE repo_id IS NULL", nil // a system-wide document
	}
	hashes, _ := json.Marshal(d.FileHashes)
	if d.FileHashes == nil {
		hashes = []byte("{}")
	}
	return scanDoc(r.s.Pool.QueryRow(ctx, `
		INSERT INTO repo_docs (id, repo_id, doc_type, doc_key, title, grp, ord, at_a_glance, sections, gaps, confidence, why, calibrated,
			inputs_hash, file_hashes, changed, source_sha, model, tokens_in, tokens_out, cost_usd, status, error, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		ON CONFLICT `+conflict+` DO UPDATE SET title = EXCLUDED.title, grp = EXCLUDED.grp, ord = EXCLUDED.ord,
			at_a_glance = EXCLUDED.at_a_glance, sections = EXCLUDED.sections, gaps = EXCLUDED.gaps, confidence = EXCLUDED.confidence,
			why = EXCLUDED.why, calibrated = EXCLUDED.calibrated, inputs_hash = EXCLUDED.inputs_hash, file_hashes = EXCLUDED.file_hashes,
			changed = EXCLUDED.changed, source_sha = EXCLUDED.source_sha, model = EXCLUDED.model, tokens_in = EXCLUDED.tokens_in,
			tokens_out = EXCLUDED.tokens_out, cost_usd = EXCLUDED.cost_usd, status = EXCLUDED.status, error = EXCLUDED.error,
			updated_at = EXCLUDED.updated_at
		RETURNING `+docCols,
		d.ID, repoID, d.Type, d.Key, d.Title, d.Group, d.Order, d.AtAGlance, j(d.Sections), j(d.Gaps), d.Confidence, j(d.Why), d.Calibrated,
		d.InputsHash, hashes, d.Changed, d.SourceSHA, d.Model, d.TokensIn, d.TokensOut, d.CostUSD, d.Status, d.Error, d.UpdatedAt))
}

// DeleteDocsExcept removes documents that no longer apply; it returns their ids.
func (r *RepoDocs) DeleteDocsExcept(ctx context.Context, repoID string, keep [][2]string) ([]string, error) {
	types, keys := make([]string, len(keep)), make([]string, len(keep))
	for i, k := range keep {
		types[i], keys[i] = k[0], k[1]
	}
	rows, err := r.s.Pool.Query(ctx, `DELETE FROM repo_docs WHERE repo_id = $1
		AND (doc_type, doc_key) NOT IN (SELECT * FROM unnest($2::text[], $3::text[])) RETURNING doc_type || '/' || doc_key`, repoID, types, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MonthCost is what writing a repository's documents cost this calendar month.
func (r *RepoDocs) MonthCost(ctx context.Context, repoID string) (float64, error) {
	var c float64
	err := r.s.Pool.QueryRow(ctx, `SELECT coalesce(sum(cost_usd), 0)::float8 FROM usage_events WHERE repo_id = $1
		AND feature IN ('docgen', 'docgen_fast', 'decide') AND at >= date_trunc('month', now())`, repoID).Scan(&c)
	return c, err
}

// DropLegacy removes the per-file generated docs of a repository (Docs v1) from the tree and the index; it
// returns the chunk ids taken out of search.
func (r *RepoDocs) DropLegacy(ctx context.Context, repoID string) ([]string, error) {
	if _, err := r.s.Pool.Exec(ctx, `DELETE FROM doc_nodes WHERE repo_id = $1`, repoID); err != nil {
		return nil, err
	}
	rows, err := r.s.Pool.Query(ctx, `UPDATE chunks SET deleted_at = now() WHERE repo_id = $1 AND source = 'generated_doc'
		AND deleted_at IS NULL AND path NOT LIKE '@docs/%' RETURNING chunk_id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, err = r.s.Pool.Exec(ctx, `UPDATE index_version SET version = version + 1`)
	}
	return ids, err
}
