// Package bootstrap implements `dth up` / `dth down` (plan § 8.19): a local stack on Docker Compose with
// generated secrets, a health wait, the owner account, and a CLI token — idempotent, so re-running it is
// the health check and the upgrade path.
package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed compose.yaml
var composeFile []byte

// ComposeFile returns the embedded Compose definition.
func ComposeFile() []byte { return composeFile }

// Options configure Up.
type Options struct {
	Dir         string // state directory (default ~/.dth)
	Image       string // hub image override
	Port        int
	OwnerEmail  string
	Ollama      bool
	Pull        bool // upgrade: pull newer images first
	WaitTimeout time.Duration
	Out         io.Writer
	// Runner executes commands (tests replace it).
	Runner func(ctx context.Context, dir string, name string, args ...string) ([]byte, error)
	// HTTP talks to the hub (tests replace it).
	HTTP *http.Client
}

// Result summarises Up.
type Result struct {
	URL           string
	OwnerEmail    string
	// SetupLink is a one-time link for the owner to choose their password (first run only). The
	// generated bootstrap password is never shown or stored, and stops working once the link is used.
	SetupLink string
	Token     string // CLI token created on the first run
}

func (o *Options) defaults() error {
	if o.Dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		o.Dir = filepath.Join(home, ".dth")
	}
	if o.Port == 0 {
		o.Port = 8080
	}
	if o.OwnerEmail == "" {
		o.OwnerEmail = "owner@localhost"
	}
	if o.WaitTimeout == 0 {
		o.WaitTimeout = 180 * time.Second
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Runner == nil {
		o.Runner = run
	}
	if o.HTTP == nil {
		jar, _ := cookiejar.New(nil)
		o.HTTP = &http.Client{Timeout: 10 * time.Second, Jar: jar}
	}
	return nil
}

func run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, out)
	}
	return out, nil
}

func secret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// readEnv parses KEY=VALUE lines.
func readEnv(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(k, "#") {
			out[k] = v
		}
	}
	return out, nil
}

func writeEnv(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("# Managed by `dth up`. Keep private: it holds the database password.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return os.WriteFile(path, b.Bytes(), 0o600)
}

func (o *Options) compose(ctx context.Context, args ...string) ([]byte, error) {
	base := []string{"compose", "-f", filepath.Join(o.Dir, "compose.yaml"), "--env-file", filepath.Join(o.Dir, ".env")}
	if o.Ollama {
		base = append(base, "--profile", "ollama")
	}
	return o.Runner(ctx, o.Dir, "docker", append(base, args...)...)
}

// Up starts (or upgrades, or health-checks) the local stack.
func Up(ctx context.Context, o Options) (Result, error) {
	if err := o.defaults(); err != nil {
		return Result{}, err
	}
	if _, err := o.Runner(ctx, "", "docker", "compose", "version"); err != nil {
		return Result{}, errors.New("Docker with the Compose plugin is required: install Docker Desktop or docker-ce, then re-run `dth up`")
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(o.Dir, "compose.yaml"), composeFile, 0o600); err != nil {
		return Result{}, err
	}
	envPath := filepath.Join(o.Dir, ".env")
	env, err := readEnv(envPath)
	if err != nil {
		return Result{}, err
	}
	res := Result{URL: fmt.Sprintf("http://localhost:%d", o.Port), OwnerEmail: o.OwnerEmail}
	first := env["DTH_DB_PASSWORD"] == ""
	var bootstrapPW string
	if first {
		env["DTH_DB_PASSWORD"] = secret(24)
		bootstrapPW = secret(24)
		env["DTH_OWNER_EMAIL"], env["DTH_OWNER_PASSWORD"] = o.OwnerEmail, bootstrapPW
	} else if env["DTH_OWNER_EMAIL"] != "" {
		res.OwnerEmail = env["DTH_OWNER_EMAIL"]
	}
	env["DTH_PORT"] = fmt.Sprint(o.Port)
	if o.Image != "" {
		env["DTH_IMAGE"] = o.Image
	}
	if err := writeEnv(envPath, env); err != nil {
		return Result{}, err
	}
	if o.Pull {
		fmt.Fprintln(o.Out, "Pulling images…")
		if _, err := o.compose(ctx, "pull"); err != nil {
			return res, err
		}
	}
	fmt.Fprintln(o.Out, "Starting Postgres and the hub…")
	if _, err := o.compose(ctx, "up", "-d", "--remove-orphans"); err != nil {
		return res, err
	}
	if err := o.waitReady(ctx, res.URL); err != nil {
		return res, err
	}
	if first {
		// The owner exists now; the bootstrap password is dropped from disk and never shown.
		delete(env, "DTH_OWNER_PASSWORD")
		if err := writeEnv(envPath, env); err != nil {
			return res, err
		}
		tok, err := o.cliToken(ctx, res.URL, res.OwnerEmail, bootstrapPW)
		if err != nil {
			return res, fmt.Errorf("hub is up, but creating a CLI token failed (run `dth-hub invite %s` in the hub container for a password link): %w", res.OwnerEmail, err)
		}
		res.Token = tok
		if res.SetupLink, err = o.setupLink(ctx, res.URL, tok); err != nil {
			return res, fmt.Errorf("hub is up, but creating your password link failed (run `dth-hub invite %s` in the hub container): %w", res.OwnerEmail, err)
		}
	}
	return res, nil
}

func (o *Options) waitReady(ctx context.Context, url string) error {
	deadline := time.Now().Add(o.WaitTimeout)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/readyz", nil)
		resp, err := o.HTTP.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the hub did not become ready within %s; check `docker compose -f %s logs hub`", o.WaitTimeout, filepath.Join(o.Dir, "compose.yaml"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// cliToken signs in as the owner and creates a personal access token for the CLI.
func (o *Options) cliToken(ctx context.Context, url, email, password string) (string, error) {
	post := func(path, csrf string, body any, out any) error {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		resp, err := o.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, msg)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	if err := post("/auth/local/login", "", map[string]string{"email": email, "password": password}, &login); err != nil {
		return "", err
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := post("/tokens", login.CSRF, map[string]any{"name": "dth CLI (dth up)", "expires_in_days": 0}, &tok); err != nil {
		return "", err
	}
	return tok.Token, nil
}

// setupLink asks the hub, as the owner, for a one-time link to set the owner's password.
func (o *Options) setupLink(ctx context.Context, url, token string) (string, error) {
	call := func(method, path string, out any) error {
		req, _ := http.NewRequestWithContext(ctx, method, url+"/api/v1"+path, bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := o.HTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			msg, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, msg)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := call(http.MethodGet, "/me", &me); err != nil {
		return "", err
	}
	var inv struct {
		Path string `json:"path"`
	}
	if err := call(http.MethodPost, "/users/"+me.ID+"/invite", &inv); err != nil {
		return "", err
	}
	return url + inv.Path, nil
}

// Down stops the stack; volumes (all data) are removed only with wipe.
func Down(ctx context.Context, o Options, wipe bool) error {
	if err := o.defaults(); err != nil {
		return err
	}
	args := []string{"down"}
	if wipe {
		args = append(args, "--volumes")
	}
	_, err := o.compose(ctx, args...)
	if err == nil && wipe {
		_ = os.Remove(filepath.Join(o.Dir, ".env"))
	}
	return err
}
