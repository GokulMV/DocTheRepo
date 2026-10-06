package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// MCPTool is one tool an MCP server offers, as last listed.
type MCPTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	// ReadOnly: the server marks the tool as not changing anything (readOnlyHint).
	ReadOnly bool `json:"read_only"`
}

// MCPServer is an MCP connection without its secrets.
type MCPServer struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	URL         string            `json:"url"`
	CatalogKey  string            `json:"catalog_key"`
	Auth        string            `json:"auth"` // none | bearer | header | oauth | aws | google
	Config      map[string]string `json:"config"`
	HasSecret   bool              `json:"has_secret"`
	SignedIn    bool              `json:"signed_in"` // oauth: a token is stored
	MinRole     string            `json:"min_role"`
	Enabled     bool              `json:"enabled"`
	Tools       []MCPTool         `json:"tools"`
	ToolChoices map[string]bool   `json:"tool_choices"`
	Status      string            `json:"status"` // new | ok | needs_sign_in | error
	LastError   string            `json:"last_error,omitempty"`
	CheckedAt   *time.Time        `json:"checked_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// ToolOn reports whether the Hub may call a tool: the admin's choice, else on only for read-only tools.
func (s MCPServer) ToolOn(t MCPTool) bool {
	if v, ok := s.ToolChoices[t.Name]; ok {
		return v
	}
	return t.ReadOnly
}

// MCPServers stores MCP connections with sealed secrets.
type MCPServers struct {
	s                   *Store
	box                 Sealer
	SecretAAD, OAuthAAD func(id string) []byte
}

// NewMCPServers returns the MCP connection store.
func NewMCPServers(s *Store, box Sealer, secretAAD, oauthAAD func(string) []byte) *MCPServers {
	return &MCPServers{s: s, box: box, SecretAAD: secretAAD, OAuthAAD: oauthAAD}
}

// NewMCPServer is a connection to create.
type NewMCPServer struct {
	Name, URL, CatalogKey, Auth, MinRole string
	Config                               map[string]string
	Secret                               string
}

// Create stores a connection.
func (m *MCPServers) Create(ctx context.Context, n NewMCPServer) (string, error) {
	id := ports.NewID()
	cfg, _ := json.Marshal(orEmpty(n.Config))
	var ct []byte
	if n.Secret != "" {
		var err error
		if ct, err = m.box.Seal(ctx, []byte(n.Secret), m.SecretAAD(id)); err != nil {
			return "", err
		}
	}
	_, err := m.s.Pool.Exec(ctx, `INSERT INTO mcp_servers (id, name, url, catalog_key, auth, config, secret_ciphertext, min_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, n.Name, n.URL, n.CatalogKey, defaultStr(n.Auth, "none"), cfg, ct, defaultStr(n.MinRole, "editor"))
	return id, err
}

const mcpCols = `id::text, name, url, catalog_key, auth, config, secret_ciphertext IS NOT NULL, oauth_ciphertext IS NOT NULL,
	min_role, enabled, tools, tool_choices, status, last_error, checked_at, created_at`

func scanMCP(row pgx.Row) (MCPServer, error) {
	var s MCPServer
	var cfg, tools, choices []byte
	var hasOAuth bool
	if err := row.Scan(&s.ID, &s.Name, &s.URL, &s.CatalogKey, &s.Auth, &cfg, &s.HasSecret, &hasOAuth, &s.MinRole, &s.Enabled,
		&tools, &choices, &s.Status, &s.LastError, &s.CheckedAt, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s, ports.ErrNotFound
		}
		return s, err
	}
	_ = json.Unmarshal(cfg, &s.Config)
	_ = json.Unmarshal(tools, &s.Tools)
	_ = json.Unmarshal(choices, &s.ToolChoices)
	if s.Config == nil {
		s.Config = map[string]string{}
	}
	if s.Tools == nil {
		s.Tools = []MCPTool{}
	}
	if s.ToolChoices == nil {
		s.ToolChoices = map[string]bool{}
	}
	s.SignedIn = s.Auth == "oauth" && hasOAuth && s.Status != "needs_sign_in"
	return s, nil
}

// List returns every connection, by name.
func (m *MCPServers) List(ctx context.Context) ([]MCPServer, error) {
	rows, err := m.s.Pool.Query(ctx, `SELECT `+mcpCols+` FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPServer{}
	for rows.Next() {
		s, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get returns one connection.
func (m *MCPServers) Get(ctx context.Context, id string) (MCPServer, error) {
	return scanMCP(m.s.Pool.QueryRow(ctx, `SELECT `+mcpCols+` FROM mcp_servers WHERE id = $1`, id))
}

// MCPPatch changes a connection; nil fields stay.
type MCPPatch struct {
	Name        *string            `json:"name"`
	URL         *string            `json:"url"`
	Config      *map[string]string `json:"config"`
	Secret      *string            `json:"secret"` // "" removes it
	MinRole     *string            `json:"min_role"`
	Enabled     *bool              `json:"enabled"`
	ToolChoices *map[string]bool   `json:"tool_choices"`
}

// Update applies a patch. Changing the address or the key drops stored sign-in tokens, which belong to the old one.
func (m *MCPServers) Update(ctx context.Context, id string, p MCPPatch) error {
	cur, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	tx, err := m.s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(q string, args ...any) {
		if err == nil {
			_, err = tx.Exec(ctx, q, args...)
		}
	}
	if p.Name != nil {
		exec(`UPDATE mcp_servers SET name = $2 WHERE id = $1`, id, *p.Name)
	}
	if p.URL != nil && *p.URL != cur.URL {
		exec(`UPDATE mcp_servers SET url = $2, oauth_ciphertext = NULL, status = 'new', tools = '[]' WHERE id = $1`, id, *p.URL)
	}
	if p.Config != nil {
		cfg, _ := json.Marshal(orEmpty(*p.Config))
		exec(`UPDATE mcp_servers SET config = $2 WHERE id = $1`, id, cfg)
	}
	if p.Secret != nil {
		var ct []byte
		if *p.Secret != "" {
			if ct, err = m.box.Seal(ctx, []byte(*p.Secret), m.SecretAAD(id)); err != nil {
				return err
			}
		}
		exec(`UPDATE mcp_servers SET secret_ciphertext = $2, status = 'new' WHERE id = $1`, id, ct)
	}
	if p.MinRole != nil {
		exec(`UPDATE mcp_servers SET min_role = $2 WHERE id = $1`, id, *p.MinRole)
	}
	if p.Enabled != nil {
		exec(`UPDATE mcp_servers SET enabled = $2 WHERE id = $1`, id, *p.Enabled)
	}
	if p.ToolChoices != nil {
		b, _ := json.Marshal(*p.ToolChoices)
		exec(`UPDATE mcp_servers SET tool_choices = $2 WHERE id = $1`, id, b)
	}
	exec(`UPDATE mcp_servers SET updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Delete removes a connection.
func (m *MCPServers) Delete(ctx context.Context, id string) error {
	tag, err := m.s.Pool.Exec(ctx, `DELETE FROM mcp_servers WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return err
}

// Secret opens the connection's key ("" when none is stored).
func (m *MCPServers) Secret(ctx context.Context, id string) (string, error) {
	var ct []byte
	if err := m.s.Pool.QueryRow(ctx, `SELECT secret_ciphertext FROM mcp_servers WHERE id = $1`, id).Scan(&ct); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	if ct == nil {
		return "", nil
	}
	b, err := m.box.Open(ctx, ct, m.SecretAAD(id))
	return string(b), err
}

// OAuth opens the connection's sealed OAuth record into out; it reports false when none is stored.
func (m *MCPServers) OAuth(ctx context.Context, id string, out any) (bool, error) {
	var ct []byte
	if err := m.s.Pool.QueryRow(ctx, `SELECT oauth_ciphertext FROM mcp_servers WHERE id = $1`, id).Scan(&ct); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ports.ErrNotFound
		}
		return false, err
	}
	if ct == nil {
		return false, nil
	}
	b, err := m.box.Open(ctx, ct, m.OAuthAAD(id))
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(b, out)
}

// SetOAuth seals and stores the OAuth record (nil removes it).
func (m *MCPServers) SetOAuth(ctx context.Context, id string, v any) error {
	var ct []byte
	if v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if ct, err = m.box.Seal(ctx, b, m.OAuthAAD(id)); err != nil {
			return err
		}
	}
	_, err := m.s.Pool.Exec(ctx, `UPDATE mcp_servers SET oauth_ciphertext = $2, updated_at = now() WHERE id = $1`, id, ct)
	return err
}

// SetStatus records the outcome of connecting; tools replaces the listed tools when not nil.
func (m *MCPServers) SetStatus(ctx context.Context, id, status, lastError string, tools []MCPTool) error {
	if tools == nil {
		_, err := m.s.Pool.Exec(ctx, `UPDATE mcp_servers SET status = $2, last_error = $3, checked_at = now() WHERE id = $1`, id, status, lastError)
		return err
	}
	b, _ := json.Marshal(tools)
	_, err := m.s.Pool.Exec(ctx, `UPDATE mcp_servers SET status = $2, last_error = $3, tools = $4, checked_at = now() WHERE id = $1`, id, status, lastError, b)
	return err
}
