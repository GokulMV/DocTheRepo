package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

// SealKeys keeps the Hub's sealing keys: one active (created on first use), and retired ones that still
// open values sealed shortly before a rotation.
type SealKeys struct {
	s    *Store
	box  *secrets.Box
	mu   sync.Mutex
	keys map[string]*secrets.SealKey
}

// NewSealKeys returns the sealing key store.
func NewSealKeys(s *Store, box *secrets.Box) *SealKeys {
	return &SealKeys{s: s, box: box, keys: map[string]*secrets.SealKey{}}
}

// RetiredGrace is how long a retired key still opens sealed values (an open browser tab sealed to it).
const RetiredGrace = 24 * time.Hour

// Active returns the active key, creating one if there is none.
func (k *SealKeys) Active(ctx context.Context) (*secrets.SealKey, error) {
	var id string
	err := k.s.Pool.QueryRow(ctx, `SELECT id FROM seal_keys WHERE retired_at IS NULL`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return k.create(ctx)
	}
	if err != nil {
		return nil, err
	}
	return k.Get(ctx, id)
}

func (k *SealKeys) create(ctx context.Context) (*secrets.SealKey, error) {
	key, err := secrets.GenerateSealKey("sk_" + ports.NewID())
	if err != nil {
		return nil, err
	}
	ct, err := k.box.Seal(ctx, key.Private(), secrets.SealKeyAAD(key.ID))
	if err != nil {
		return nil, err
	}
	pub := key.Public()
	tag, err := k.s.Pool.Exec(ctx, `INSERT INTO seal_keys (id, x25519_pub, mlkem_pub, private_ct) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		key.ID, pub.X25519, pub.MLKEM768, ct)
	if err != nil {
		return nil, fmt.Errorf("store seal key: %w", err)
	}
	if tag.RowsAffected() == 0 { // another replica created one first
		return k.Active(ctx)
	}
	k.mu.Lock()
	k.keys[key.ID] = key
	k.mu.Unlock()
	return key, nil
}

// Get loads a key by id: the active one, or one retired less than RetiredGrace ago.
func (k *SealKeys) Get(ctx context.Context, id string) (*secrets.SealKey, error) {
	k.mu.Lock()
	cached := k.keys[id]
	k.mu.Unlock()
	var ct []byte
	var retired *time.Time
	err := k.s.Pool.QueryRow(ctx, `SELECT private_ct, retired_at FROM seal_keys WHERE id = $1`, id).Scan(&ct, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, secrets.ErrSealed
	}
	if err != nil {
		return nil, err
	}
	if retired != nil && time.Since(*retired) > RetiredGrace {
		return nil, secrets.ErrSealed
	}
	if cached != nil {
		return cached, nil
	}
	priv, err := k.box.Open(ctx, ct, secrets.SealKeyAAD(id))
	if err != nil {
		return nil, err
	}
	defer clear(priv)
	key, err := secrets.LoadSealKey(id, priv)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.keys[id] = key
	k.mu.Unlock()
	return key, nil
}

// Rotate retires the active key and creates a new one.
func (k *SealKeys) Rotate(ctx context.Context) (*secrets.SealKey, error) {
	if _, err := k.s.Pool.Exec(ctx, `UPDATE seal_keys SET retired_at = now() WHERE retired_at IS NULL`); err != nil {
		return nil, err
	}
	return k.create(ctx)
}

// Unseal opens a sealed value for purpose; any other value is returned as it is.
func (k *SealKeys) Unseal(ctx context.Context, v, purpose string) (string, error) {
	if !secrets.IsSealed(v) {
		return v, nil
	}
	key, err := k.Get(ctx, secrets.SealedKeyID(v))
	if err != nil {
		return "", err
	}
	b, err := key.Unseal(v, purpose)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SecretHint is what may be shown about a write-only secret: the last 4 characters of a long value (an API
// key or token), nothing for short or structured ones.
func SecretHint(v string) string {
	if len(v) < 24 || len(v) > 512 || v[0] == '{' || v[0] == '-' {
		return ""
	}
	return v[len(v)-4:]
}

// SetProviderKeyHint records a provider key's hint and time.
func (k *SealKeys) SetProviderKeyHint(ctx context.Context, id, secret string) error {
	_, err := k.s.Pool.Exec(ctx, `UPDATE llm_providers SET key_hint = $2, key_set_at = now() WHERE id = $1`, id, SecretHint(secret))
	return err
}

// SetConnectorCredsHint records a connector credential's hint and time.
func (k *SealKeys) SetConnectorCredsHint(ctx context.Context, id, secret string) error {
	_, err := k.s.Pool.Exec(ctx, `UPDATE connectors SET creds_hint = $2, creds_set_at = now() WHERE id = $1`, id, SecretHint(secret))
	return err
}

// SecretMeta is a write-only secret's hint and when it was set.
type SecretMeta struct {
	Hint  string     `json:"hint,omitempty"`
	SetAt *time.Time `json:"set_at,omitempty"`
}

// Hints returns provider ("llm_providers") or connector ("connectors") secret metadata by id.
func (k *SealKeys) Hints(ctx context.Context, table string) (map[string]SecretMeta, error) {
	var q string
	switch table {
	case "llm_providers":
		q = `SELECT id, key_hint, key_set_at FROM llm_providers`
	case "connectors":
		q = `SELECT id, creds_hint, creds_set_at FROM connectors`
	default:
		return nil, fmt.Errorf("unknown table %q", table)
	}
	rows, err := k.s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SecretMeta{}
	for rows.Next() {
		var id string
		var m SecretMeta
		if err := rows.Scan(&id, &m.Hint, &m.SetAt); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}
