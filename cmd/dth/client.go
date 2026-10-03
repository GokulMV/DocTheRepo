package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cliConfig is ~/.dth/cli.json.
type cliConfig struct {
	Server string `json:"server"`
	Token  string `json:"token"`
	// Profiles are named hubs (nonlive, production, …); Current is the one used when no --profile is given.
	Profiles map[string]cliProfile `json:"profiles,omitempty"`
	Current  string                `json:"current,omitempty"`
}

// cliProfile is one named hub.
type cliProfile struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

// resolve returns the hub of the named profile, the current one, or the unnamed default.
func (c cliConfig) resolve(name string) (cliProfile, error) {
	if name == "" {
		name = c.Current
	}
	if name == "" {
		return cliProfile{Server: c.Server, Token: c.Token}, nil
	}
	p, ok := c.Profiles[name]
	if !ok {
		return cliProfile{}, fmt.Errorf("no profile %q (sign in with `dth login --profile %s --server … --token …`)", name, name)
	}
	return p, nil
}

// withProfile stores server and token under name ("": the unnamed default), keeping every other profile.
func (c cliConfig) withProfile(name, server, token string) cliConfig {
	if name == "" {
		c.Server, c.Token = server, token
		return c
	}
	if c.Profiles == nil {
		c.Profiles = map[string]cliProfile{}
	}
	c.Profiles[name] = cliProfile{Server: server, Token: token}
	if c.Current == "" {
		c.Current = name
	}
	return c
}

func configPath() string {
	if p := os.Getenv("DTH_CLI_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dth", "cli.json")
}

func loadConfig() cliConfig {
	var c cliConfig
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(c cliConfig) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// client calls the hub API with a personal access token.
type client struct {
	server string
	token  string
	http   *http.Client
}

// apiError is the hub's error contract.
type apiError struct {
	Status        int
	Code, Message string
	CorrelationID string
}

func (e *apiError) Error() string {
	s := fmt.Sprintf("%s: %s", e.Code, e.Message)
	if e.CorrelationID != "" {
		s += " (ref " + e.CorrelationID + ")"
	}
	return s
}

func (c *client) request(ctx context.Context, method, path string, body any, accept string) (*http.Response, error) {
	if c.token == "" {
		return nil, errors.New("not signed in: run `dth login --server <url> --token <token>` (or `dth up` locally), or set DTH_TOKEN")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.server, "/")+"/api/v1"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", c.server, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Code, Message string
				CorrelationID string `json:"correlation_id"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error.Code == "" {
			e.Error.Code, e.Error.Message = fmt.Sprintf("HTTP_%d", resp.StatusCode), resp.Status
		}
		return nil, &apiError{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message, CorrelationID: e.Error.CorrelationID}
	}
	return resp, nil
}

// call sends a JSON request and decodes a JSON response into out (skipped when nil or 204).
func (c *client) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := c.request(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func newClient(server, token string) *client {
	return &client{server: server, token: token, http: &http.Client{Timeout: 5 * time.Minute}}
}
