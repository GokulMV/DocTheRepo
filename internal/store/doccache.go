package store

import (
	"context"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/docrouter"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// DocCache is generated doc sections by the content they describe (docrouter.Cache).
type DocCache struct{ s *Store }

// NewDocCache returns the doc cache.
func NewDocCache(s *Store) *DocCache { return &DocCache{s: s} }

// Get returns cached bodies for the keys that have one.
func (c *DocCache) Get(ctx context.Context, keys []docrouter.Key) (map[docrouter.Key]string, error) {
	out := map[docrouter.Key]string{}
	if len(keys) == 0 {
		return out, nil
	}
	p := gen.GetDocCacheParams{}
	for _, k := range keys {
		p.Hashes, p.Symbols = append(p.Hashes, k.ContentHash), append(p.Symbols, k.Symbol)
	}
	rows, err := c.s.Q.GetDocCache(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[docrouter.Key{ContentHash: r.ContentHash, Symbol: r.Symbol}] = r.Body
	}
	if len(rows) > 0 {
		_ = c.s.Q.TouchDocCache(ctx, gen.TouchDocCacheParams{Hashes: p.Hashes, Symbols: p.Symbols})
	}
	return out, nil
}

// Put stores generated bodies.
func (c *DocCache) Put(ctx context.Context, model string, bodies map[docrouter.Key]string) error {
	for k, b := range bodies {
		if err := c.s.Q.PutDocCache(ctx, gen.PutDocCacheParams{ContentHash: k.ContentHash, Symbol: k.Symbol, Body: b, Model: model}); err != nil {
			return err
		}
	}
	return nil
}

// GC drops entries not used since cutoff.
func (c *DocCache) GC(ctx context.Context, cutoff time.Time) (int64, error) {
	return c.s.Q.GCDocCache(ctx, cutoff)
}

// DocumentedChunks returns the chunks of a repository that have a generated doc section.
func (d *Docs) DocumentedChunks(ctx context.Context, repoID string) (map[string]bool, error) {
	ids, err := d.s.Q.DocumentedChunkIDs(ctx, repoID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// AppSettings are hub-wide settings changed in the UI.
type AppSettings struct{ s *Store }

// NewAppSettings returns the settings store.
func NewAppSettings(s *Store) *AppSettings { return &AppSettings{s: s} }

// Get returns a setting, or "" when it was never set.
func (a *AppSettings) Get(ctx context.Context, key string) (string, error) {
	v, err := a.s.Q.GetAppSetting(ctx, key)
	if IsNoRows(err) {
		return "", nil
	}
	return v, err
}

// Set stores a setting.
func (a *AppSettings) Set(ctx context.Context, key, value string) error {
	return a.s.Q.SetAppSetting(ctx, gen.SetAppSettingParams{Key: key, Value: value})
}
