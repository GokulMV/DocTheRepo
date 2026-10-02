package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/settings"
)

// startupSettingsRetry is how long to wait between attempts when applying fails (an identity provider
// or git host that is not reachable yet).
var startupSettingsRetry = 30 * time.Second

// applyStartupSettings applies the settings files named in the config (DTH_SETTINGS_FILE, DTH_SETTINGS)
// through the Hub's own API, acting as the Hub itself with the owner role. Applying is idempotent, so it
// runs on every start; a failure is logged and retried, and never stops the Hub.
func applyStartupSettings(ctx context.Context, cfg config.SettingsConfig, h http.Handler, log *slog.Logger) {
	if len(cfg.ApplyOnStart) == 0 && cfg.Inline == "" {
		return
	}
	run := func() error {
		srcs, err := settings.ReadPaths(withFiles(cfg.ApplyOnStart), nil)
		if err != nil {
			return err
		}
		if len(srcs) == 0 && cfg.Inline == "" {
			return nil // only empty settings directories (Compose mounts one by default)
		}
		if cfg.Inline != "" {
			srcs = append(srcs, settings.Source{Name: "DTH_SETTINGS", Data: []byte(cfg.Inline)})
		}
		doc, err := settings.Load(ctx, srcs, &settings.Resolver{})
		if err != nil {
			return err
		}
		api := &settings.HTTPAPI{Base: "http://hub.internal/api/v1", Client: settings.HandlerClient(h)}
		res, err := settings.Apply(auth.WithPrincipal(ctx, auth.SystemPrincipal("settings file")), api, doc, false)
		c, u, n := res.Counts()
		if err != nil {
			return err
		}
		if len(res.Changes) > 0 {
			log.Info("applied settings at startup", "created", c, "updated", u, "unchanged", n)
		}
		return nil
	}
	go func() {
		for attempt := 1; ; attempt++ {
			err := run()
			if err == nil {
				return
			}
			log.Warn("settings at startup not fully applied; retrying", "attempt", attempt, "err", err)
			if attempt == 20 {
				log.Error("giving up on applying settings at startup; fix the file and restart, or apply it with dth apply", "err", err)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(startupSettingsRetry):
			}
		}
	}()
}

// withFiles drops directories that hold no settings files, so an empty mounted directory is not an error.
func withFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		ents, err := os.ReadDir(p)
		if err != nil { // a file, or missing: let ReadPaths report it
			out = append(out, p)
			continue
		}
		for _, e := range ents {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !e.IsDir() && (ext == ".yaml" || ext == ".yml" || ext == ".json") {
				out = append(out, p)
				break
			}
		}
	}
	return out
}
