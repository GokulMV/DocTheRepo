package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AtlassianApp is the Atlassian OAuth app the operator registered once (client secret decrypted).
type AtlassianApp struct {
	ClientID     string
	ClientSecret string
	UpdatedAt    time.Time
}

// AtlassianApps stores the Atlassian OAuth app (one row) with its client secret sealed.
type AtlassianApps struct {
	s   *Store
	box Sealer
	aad []byte
}

// NewAtlassianApps returns the store; aad binds the secret's ciphertext (secrets.AtlassianSecretAAD).
func NewAtlassianApps(s *Store, box Sealer, aad []byte) *AtlassianApps {
	return &AtlassianApps{s: s, box: box, aad: aad}
}

// Get returns the app; ok is false when none is configured.
func (a *AtlassianApps) Get(ctx context.Context) (app AtlassianApp, ok bool, err error) {
	var ct []byte
	err = a.s.Pool.QueryRow(ctx, `SELECT client_id, client_secret_ct, updated_at FROM atlassian_oauth_app WHERE id = 1`).
		Scan(&app.ClientID, &ct, &app.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AtlassianApp{}, false, nil
	}
	if err != nil {
		return AtlassianApp{}, false, err
	}
	b, err := a.box.Open(ctx, ct, a.aad)
	if err != nil {
		return AtlassianApp{}, false, err
	}
	app.ClientSecret = string(b)
	return app, true, nil
}

// Set stores the app. An empty secret keeps the stored one (an error when there is none).
func (a *AtlassianApps) Set(ctx context.Context, clientID, secret string) error {
	if secret == "" {
		tag, err := a.s.Pool.Exec(ctx, `UPDATE atlassian_oauth_app SET client_id = $1, updated_at = now() WHERE id = 1`, clientID)
		if err == nil && tag.RowsAffected() == 0 {
			err = errors.New("the client secret is required")
		}
		return err
	}
	ct, err := a.box.Seal(ctx, []byte(secret), a.aad)
	if err != nil {
		return err
	}
	_, err = a.s.Pool.Exec(ctx, `INSERT INTO atlassian_oauth_app (id, client_id, client_secret_ct) VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET client_id = EXCLUDED.client_id, client_secret_ct = EXCLUDED.client_secret_ct, updated_at = now()`, clientID, ct)
	return err
}

// Delete removes the app (existing OAuth connectors then cannot refresh until it is set again).
func (a *AtlassianApps) Delete(ctx context.Context) error {
	_, err := a.s.Pool.Exec(ctx, `DELETE FROM atlassian_oauth_app`)
	return err
}
