// Package codehost builds CodeHost adapters from stored git connectors and caches them per connector.
package codehost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost/github"
	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost/gitlab"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Build constructs the adapter for a connector. Connector config keys: base_url, and for GitHub either
// auth=token or app_id + installation_id (credentials then hold the App private key); group for GitLab
// repository discovery. Credentials hold the token or key; the webhook secret comes from the connector.
func Build(cc ports.ConnectorConfig) (ports.CodeHost, error) {
	extra := map[string]string{}
	for k, v := range cc.Config {
		extra[k] = v
	}
	extra["webhook_secret"] = cc.WebhookSecret
	pc := ports.ProviderConfig{ID: cc.ID, Kind: cc.Type, Name: cc.Name, BaseURL: cc.Config["base_url"], APIKey: cc.Credentials, Extra: extra}
	switch cc.Type {
	case "github":
		if extra["app_id"] == "" && extra["auth"] == "" {
			extra["auth"] = "token"
		}
		return github.New(pc, nil)
	case "gitlab":
		return gitlab.New(pc)
	}
	return nil, ports.Permanent(fmt.Errorf("connector %q (%s) is not a git host", cc.Name, cc.Type))
}

// Loader loads a connector with decrypted secrets.
type Loader func(ctx context.Context, id string) (ports.ConnectorConfig, error)

// Factory caches adapters per connector so webhook verification and jobs reuse clients (and GitHub App
// installation tokens). Entries expire after TTL so rotated credentials are picked up.
type Factory struct {
	Load Loader
	TTL  time.Duration
	// Build overrides adapter construction (tests).
	Build func(ports.ConnectorConfig) (ports.CodeHost, error)
	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	host ports.CodeHost
	cc   ports.ConnectorConfig
	at   time.Time
}

// Host returns the adapter for a connector.
func (f *Factory) Host(ctx context.Context, connectorID string) (ports.CodeHost, error) {
	h, _, err := f.HostAndConfig(ctx, connectorID)
	return h, err
}

// HostAndConfig returns the adapter and the connector config it was built from.
func (f *Factory) HostAndConfig(ctx context.Context, connectorID string) (ports.CodeHost, ports.ConnectorConfig, error) {
	ttl := f.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	f.mu.Lock()
	if e, ok := f.cache[connectorID]; ok && time.Since(e.at) < ttl {
		f.mu.Unlock()
		return e.host, e.cc, nil
	}
	f.mu.Unlock()
	cc, err := f.Load(ctx, connectorID)
	if err != nil {
		return nil, cc, err
	}
	build := f.Build
	if build == nil {
		build = Build
	}
	h, err := build(cc)
	if err != nil {
		return nil, cc, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		f.cache = map[string]entry{}
	}
	f.cache[connectorID] = entry{host: h, cc: cc, at: time.Now()}
	return h, cc, nil
}

// Invalidate drops a cached adapter (connector edited or deleted).
func (f *Factory) Invalidate(connectorID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cache, connectorID)
}
