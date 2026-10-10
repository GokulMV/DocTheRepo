// Package config loads and validates the Hub's bootstrap configuration: a YAML file whose every key has
// a working default, overridden by DTH_* environment variables. Runtime settings that operators edit
// from the UI (connectors, providers, routes, limits) live in the database, not here.
package config

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Role is one of the three process roles the binary can run.
type Role string

const (
	RoleAPI       Role = "api"
	RoleWorker    Role = "worker"
	RoleScheduler Role = "scheduler"
)

// Config is the complete bootstrap configuration.
type Config struct {
	Roles     []Role          `yaml:"roles"`
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Secrets   SecretsConfig   `yaml:"secrets"`
	Auth      AuthConfig      `yaml:"auth"`
	Queue     QueueConfig     `yaml:"queue"`
	Logging   LoggingConfig   `yaml:"logging"`
	Tracing   TracingConfig   `yaml:"tracing"`
	Docs      DocsConfig      `yaml:"docs"`
	Spend     SpendConfig     `yaml:"spend"`
	Retention RetentionConfig `yaml:"retention"`
	Grammars  GrammarsConfig  `yaml:"grammars"`
	Vector    VectorConfig    `yaml:"vector"`
	Decide    DecideConfig    `yaml:"decide"`
	Settings  SettingsConfig  `yaml:"settings"`
	Ask       AskConfig       `yaml:"ask"`
	Email     EmailConfig     `yaml:"email"`
}

// EmailConfig sends invite and password links by email. Without it the admin copies the link and passes
// it on. Any SMTP provider works; see docs/users-and-sign-in.md.
type EmailConfig struct {
	// SMTPURLEnv names the variable holding the SMTP URL (default DTH_SMTP_URL), since it carries the
	// password: smtp://user:password@host:587 (STARTTLS) or smtps://user:password@host:465.
	SMTPURLEnv string `yaml:"smtp_url_env"`
	// From is the sender, e.g. "DocTheRepo <docs@example.com>" (DTH_EMAIL_FROM).
	From string `yaml:"from"`
}

// SMTPURL is the SMTP URL from the environment ("" when email is off).
func (e EmailConfig) SMTPURL() string {
	if e.SMTPURLEnv == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(e.SMTPURLEnv))
}

// AskConfig tunes Ask.
type AskConfig struct {
	// AgentSteps is how many steps the model may take to look further (search again, read a file, list
	// files) when one round of retrieval finds too little (DTH_ASK_AGENT_STEPS; 0 turns it off). Answers
	// found that way are cached like any other.
	AgentSteps int `yaml:"agent_steps"`
	// SimilarAnswer is how alike (embedding cosine similarity) a reworded question must be to reuse a
	// cached answer (DTH_ASK_SIMILAR_ANSWER; default 0.95, 0 turns it off). Questions naming different
	// files or identifiers never share an answer.
	SimilarAnswer float64 `yaml:"similar_answer"`
	// Sift turns on the source picker (DTH_ASK_SIFT: on, the default, or off): a cheap judge model (the
	// sift route, else decide, else docgen_fast) keeps only the retrieved sources an answer needs, and
	// explores the index before the agent. It skips itself when the judge would not be cheaper.
	Sift string `yaml:"sift"`
	// SiftKeepAt is the probability of relevance a source needs to be kept (DTH_ASK_SIFT_KEEP_AT;
	// default 0.5). Lower keeps more.
	SiftKeepAt float64 `yaml:"sift_keep_at"`
}

// SettingsConfig is the policy for settings files pasted into the UI (Administration → Settings file).
// Their ${…} secret references are resolved by the Hub itself, so every source is off unless listed.
type SettingsConfig struct {
	// SecretSources are the enabled reference schemes: env, file, vault, gopass, awssm, gcpsm.
	SecretSources []string `yaml:"secret_sources"`
	// EnvPrefix is required at the start of ${env:NAME} (default DTH_SECRET_).
	EnvPrefix string `yaml:"env_prefix"`
	// FileRoot is the only directory ${file:…} may read (file stays off without it).
	FileRoot string `yaml:"file_root"`
	// RequireSealed refuses provider keys and connector secrets that are not sealed to the Hub's key.
	RequireSealed bool `yaml:"require_sealed_secrets"`
	// ApplyOnStart are settings files or directories applied when the Hub starts (DTH_SETTINGS_FILE,
	// comma-separated), so a deployment comes up with sign-in, users, models and repositories in place.
	// They are the operator's own files: every secret reference scheme works in them.
	ApplyOnStart []string `yaml:"apply_on_start"`
	// Inline is a settings document applied at start, for platforms where mounting a file is awkward
	// (DTH_SETTINGS; ECS and Cloud Run can fill it from a secret).
	Inline string `yaml:"-"`
}

// DecideConfig controls decisions behind confidence gates (plan Phase 11.5). They only run when a
// "decide" model route is configured.
type DecideConfig struct {
	// GateThreshold is the minimum probability at which a decision replaces the full model path.
	GateThreshold float64 `yaml:"gate_threshold"`
}

// ServerConfig controls the HTTP listeners.
type ServerConfig struct {
	Listen        string        `yaml:"listen"`
	MetricsListen string        `yaml:"metrics_listen"`
	PublicURL     string        `yaml:"public_url"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
	// ShutdownGrace bounds how long in-flight requests and jobs get on SIGTERM.
	ShutdownGrace time.Duration `yaml:"shutdown_grace"`
	// Environment names this deployment (DTH_ENVIRONMENT), e.g. nonlive or production. Anything but
	// production (or empty) shows a banner in the UI so nobody mistakes one environment for another.
	Environment string `yaml:"environment"`
}

// DatabaseConfig points at PostgreSQL. The URL is read from the named env var so it never sits in the file.
type DatabaseConfig struct {
	URLEnv           string        `yaml:"url_env"`
	MaxConns         int32         `yaml:"max_conns"`
	ConnectTimeout   time.Duration `yaml:"connect_timeout"`
	MigrateOnStart   bool          `yaml:"migrate_on_start"`
	StatementTimeout time.Duration `yaml:"statement_timeout"`
}

// URL resolves the database URL from the environment.
func (d DatabaseConfig) URL() string { return os.Getenv(d.URLEnv) }

// SecretsConfig selects the key-encryption-key provider for envelope encryption.
type SecretsConfig struct {
	Provider     string `yaml:"provider"` // localfile | awskms | gcpkms
	LocalKeyFile string `yaml:"local_key_file"`
	KMSKeyID     string `yaml:"kms_key_id"`
}

// AuthConfig controls user authentication.
type AuthConfig struct {
	Mode                 string        `yaml:"mode"` // local | oidc
	OIDC                 OIDCConfig    `yaml:"oidc"`
	SessionIdle          time.Duration `yaml:"session_idle"`
	SessionAbsolute      time.Duration `yaml:"session_absolute"`
	AllUsersReadAllRepos bool          `yaml:"all_users_read_all_repos"`
}

// OIDCConfig is an OpenID Connect client (Okta, Google Workspace, Entra ID, Keycloak).
type OIDCConfig struct {
	Issuer          string   `yaml:"issuer"`
	ClientID        string   `yaml:"client_id"`
	ClientSecretEnv string   `yaml:"client_secret_env"`
	RedirectURL     string   `yaml:"redirect_url"`
	Scopes          []string `yaml:"scopes"`
	GroupsClaim     string   `yaml:"groups_claim"`
	AllowedDomains  []string `yaml:"allowed_domains"`
}

// QueueConfig tunes the Postgres job queue and worker pool.
type QueueConfig struct {
	Concurrency  map[string]int `yaml:"concurrency"`
	LeaseTTL     time.Duration  `yaml:"lease_ttl"`
	MaxAttempts  int            `yaml:"max_attempts"`
	PollInterval time.Duration  `yaml:"poll_interval"`
	BackoffBase  time.Duration  `yaml:"backoff_base"`
	BackoffMax   time.Duration  `yaml:"backoff_max"`
}

// LoggingConfig controls slog output.
type LoggingConfig struct {
	Format string `yaml:"format"` // json | text
	Level  string `yaml:"level"`  // debug | info | warn | error
}

// TracingConfig controls optional OpenTelemetry export.
type TracingConfig struct {
	Enabled      bool   `yaml:"enabled"`
	OTLPEndpoint string `yaml:"otlp_endpoint"`
	ServiceName  string `yaml:"service_name"`
}

// DocsConfig holds the default generated-docs path for newly connected repos.
type DocsConfig struct {
	DefaultPath string `yaml:"default_path"`
	// PRSweepInterval is how often the scheduler merges green docs PRs, rebases conflicted ones, and
	// closes stale ones.
	PRSweepInterval time.Duration `yaml:"pr_sweep_interval"`
	// GenerationMode trades thoroughness for cost (DTH_DOCS_MODE): thorough, balanced (default) or
	// economy. See docs/docs-generation.md.
	GenerationMode string `yaml:"generation_mode"`
	// Version picks how docs are written (DTH_DOCS_VERSION): 2 (default) writes readable documents per
	// repository (overview, architecture, module guides and more) kept in the Hub; 1 writes one doc per
	// source file and lands it as a docs PR.
	Version int `yaml:"version"`
	// RepoMonthlyCapUSD caps what writing one repository's documents may cost per calendar month
	// (0: the cap is set from the first estimate). Docs pause when it is reached.
	RepoMonthlyCapUSD float64 `yaml:"repo_monthly_cap_usd"`
}

// SpendConfig holds the acknowledgements that guard the spend ceilings (limits themselves live in the DB).
type SpendConfig struct {
	AllowUnlimited       bool `yaml:"allow_unlimited"`
	AllowUnreportedUsage bool `yaml:"allow_unreported_usage"`
	// BufferPct is the safety buffer kept free under every dollar ceiling and docs budget, in percent of it
	// (DTH_SPEND_BUFFER_PCT; default 5, at least $0.01; 0 turns it off). Calls stop at cap - buffer.
	BufferPct float64 `yaml:"buffer_pct"`
}

// RetentionConfig controls garbage collection windows.
type RetentionConfig struct {
	EventDays   int `yaml:"event_days"`
	ChunkGCDays int `yaml:"chunk_gc_days"`
}

// VectorConfig selects the vector index adapter (plan § 3: pgvector default, Qdrant alternative).
type VectorConfig struct {
	Backend      string `yaml:"backend"` // pgvector | qdrant
	QdrantURL    string `yaml:"qdrant_url"`
	QdrantKeyEnv string `yaml:"qdrant_api_key_env"`
}

// QdrantKey reads the Qdrant API key from the configured env var.
func (v VectorConfig) QdrantKey() string {
	if v.QdrantKeyEnv == "" {
		return ""
	}
	return os.Getenv(v.QdrantKeyEnv)
}

// GrammarsConfig points at runtime-loaded Tree-sitter grammars.
type GrammarsConfig struct {
	LoadDir string `yaml:"load_dir"`
}

// Default returns a configuration in which every key has a working value.
func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Roles: []Role{RoleAPI, RoleWorker, RoleScheduler},
		Server: ServerConfig{
			Listen: "127.0.0.1:8080", MetricsListen: "127.0.0.1:9090",
			ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, ShutdownGrace: 30 * time.Second,
		},
		Database: DatabaseConfig{
			URLEnv: "DTH_DATABASE_URL", MaxConns: 20, ConnectTimeout: 10 * time.Second,
			MigrateOnStart: true, StatementTimeout: 60 * time.Second,
		},
		Secrets: SecretsConfig{Provider: "localfile", LocalKeyFile: filepath.Join(home, ".dth", "master.key")},
		Email:   EmailConfig{SMTPURLEnv: "DTH_SMTP_URL"},
		Auth: AuthConfig{
			Mode: "local",
			OIDC: OIDCConfig{
				ClientSecretEnv: "DTH_OIDC_CLIENT_SECRET",
				Scopes:          []string{"openid", "email", "profile"},
				GroupsClaim:     "groups",
			},
			SessionIdle: 12 * time.Hour, SessionAbsolute: 7 * 24 * time.Hour, AllUsersReadAllRepos: true,
		},
		Queue: QueueConfig{
			Concurrency: map[string]int{
				"code_push": 4, "decode_issue": 4, "knowledge_sync": 2, "security_scan": 1, "security_fix": 1,
				"import_docs": 1, "reindex": 1, "signal_batch": 8, "pr_review": 2, "repo_docs": 2, "system_docs": 1,
			},
			LeaseTTL: 5 * time.Minute, MaxAttempts: 5, PollInterval: time.Second,
			BackoffBase: time.Second, BackoffMax: 5 * time.Minute,
		},
		Logging:   LoggingConfig{Format: "json", Level: "info"},
		Tracing:   TracingConfig{ServiceName: "dth-hub"},
		Docs:      DocsConfig{DefaultPath: "docs/generated/", PRSweepInterval: 5 * time.Minute, Version: 2},
		Vector:    VectorConfig{Backend: "pgvector", QdrantKeyEnv: "DTH_QDRANT_API_KEY"},
		Retention: RetentionConfig{EventDays: 30, ChunkGCDays: 14},
		Spend:     SpendConfig{BufferPct: 5},
		Grammars:  GrammarsConfig{LoadDir: "./grammars"},
		Decide:    DecideConfig{GateThreshold: 0.9},
		Ask:       AskConfig{AgentSteps: 4, SimilarAnswer: 0.95, Sift: "on", SiftKeepAt: 0.5},
	}
}

// Load reads path (if it exists) over the defaults, applies environment overrides, and validates.
// A missing file is not an error: the defaults are a complete configuration.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		default:
			dec := yaml.NewDecoder(strings.NewReader(string(raw)))
			dec.KnownFields(true)                                               // a typo in a key must fail loudly, not silently fall back to a default
			if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) { // empty file = all defaults
				return cfg, fmt.Errorf("parse config %s: %w", path, err)
			}
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	cfg.normalize()
	return cfg, cfg.Validate()
}

// applyEnv maps DTH_* variables onto the keys operators most often set per environment.
func applyEnv(cfg *Config) error {
	str := map[string]*string{
		"DTH_LISTEN":              &cfg.Server.Listen,
		"DTH_METRICS_LISTEN":      &cfg.Server.MetricsListen,
		"DTH_PUBLIC_URL":          &cfg.Server.PublicURL,
		"DTH_ENVIRONMENT":         &cfg.Server.Environment,
		"DTH_SECRETS_PROVIDER":    &cfg.Secrets.Provider,
		"DTH_LOCAL_KEY_FILE":      &cfg.Secrets.LocalKeyFile,
		"DTH_KMS_KEY_ID":          &cfg.Secrets.KMSKeyID,
		"DTH_AUTH_MODE":           &cfg.Auth.Mode,
		"DTH_OIDC_ISSUER":         &cfg.Auth.OIDC.Issuer,
		"DTH_OIDC_CLIENT_ID":      &cfg.Auth.OIDC.ClientID,
		"DTH_OIDC_REDIRECT_URL":   &cfg.Auth.OIDC.RedirectURL,
		"DTH_LOG_FORMAT":          &cfg.Logging.Format,
		"DTH_LOG_LEVEL":           &cfg.Logging.Level,
		"DTH_OTLP_ENDPOINT":       &cfg.Tracing.OTLPEndpoint,
		"DTH_GRAMMARS_DIR":        &cfg.Grammars.LoadDir,
		"DTH_VECTOR_BACKEND":      &cfg.Vector.Backend,
		"DTH_QDRANT_URL":          &cfg.Vector.QdrantURL,
		"DTH_SETTINGS_ENV_PREFIX": &cfg.Settings.EnvPrefix,
		"DTH_SETTINGS_FILE_ROOT":  &cfg.Settings.FileRoot,
		"DTH_EMAIL_FROM":          &cfg.Email.From,
	}
	for k, dst := range str {
		if v, ok := os.LookupEnv(k); ok {
			*dst = v
		}
	}
	if v, ok := os.LookupEnv("DTH_ROLES"); ok {
		cfg.Roles = nil
		for _, r := range splitList(v) {
			cfg.Roles = append(cfg.Roles, Role(r))
		}
	}
	if v, ok := os.LookupEnv("DTH_REQUIRE_SEALED_SECRETS"); ok {
		cfg.Settings.RequireSealed = v == "1" || strings.EqualFold(v, "true")
	}
	if v, ok := os.LookupEnv("DTH_SETTINGS_FILE"); ok {
		cfg.Settings.ApplyOnStart = splitList(v)
	}
	if v, ok := os.LookupEnv("DTH_SETTINGS"); ok {
		cfg.Settings.Inline = v
	}
	if v, ok := os.LookupEnv("DTH_SETTINGS_SECRET_SOURCES"); ok {
		cfg.Settings.SecretSources = splitList(v)
	}
	if v, ok := os.LookupEnv("DTH_OIDC_ALLOWED_DOMAINS"); ok {
		cfg.Auth.OIDC.AllowedDomains = splitList(v)
	}
	if v, ok := os.LookupEnv("DTH_DOCS_VERSION"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Docs.Version = n
		}
	}
	if v, ok := os.LookupEnv("DTH_DOCS_MODE"); ok {
		cfg.Docs.GenerationMode = v
	}
	if v, ok := os.LookupEnv("DTH_ASK_AGENT_STEPS"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 10 {
			return fmt.Errorf("DTH_ASK_AGENT_STEPS: want 0 to 10, got %q", v)
		}
		cfg.Ask.AgentSteps = n
	}
	if v, ok := os.LookupEnv("DTH_ASK_SIMILAR_ANSWER"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("DTH_ASK_SIMILAR_ANSWER: want 0 (off) or a similarity from 0.8 to 1, got %q", v)
		}
		cfg.Ask.SimilarAnswer = f
	}
	if v, ok := os.LookupEnv("DTH_ASK_SIFT"); ok {
		cfg.Ask.Sift = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := os.LookupEnv("DTH_ASK_SIFT_KEEP_AT"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("DTH_ASK_SIFT_KEEP_AT: want a probability from 0.1 to 0.95, got %q", v)
		}
		cfg.Ask.SiftKeepAt = f
	}
	if v, ok := os.LookupEnv("DTH_SPEND_BUFFER_PCT"); ok {
		f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "%"), 64)
		if err != nil {
			return fmt.Errorf("DTH_SPEND_BUFFER_PCT: want a percentage from 0 to 50, got %q", v)
		}
		cfg.Spend.BufferPct = f
	}
	if v, ok := os.LookupEnv("DTH_PR_SWEEP_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return fmt.Errorf("DTH_PR_SWEEP_INTERVAL: want a positive duration like 5m, got %q", v)
		}
		cfg.Docs.PRSweepInterval = d
	}
	if v, ok := os.LookupEnv("DTH_ALL_USERS_READ_ALL_REPOS"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("DTH_ALL_USERS_READ_ALL_REPOS: %w", err)
		}
		cfg.Auth.AllUsersReadAllRepos = b
	}
	if v, ok := os.LookupEnv("DTH_TRACING_ENABLED"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("DTH_TRACING_ENABLED: %w", err)
		}
		cfg.Tracing.Enabled = b
	}
	return nil
}

// splitList parses a comma-separated env value, dropping blanks.
func splitList(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func (c *Config) normalize() {
	if c.Docs.DefaultPath != "" && !strings.HasSuffix(c.Docs.DefaultPath, "/") {
		c.Docs.DefaultPath += "/"
	}
	// Job types added after a config file was written run with one worker unless the file says otherwise.
	for t, n := range map[string]int{"security_scan": 1, "security_fix": 1, "repo_docs": 2, "system_docs": 1} {
		if _, set := c.Queue.Concurrency[t]; !set && c.Queue.Concurrency != nil {
			c.Queue.Concurrency[t] = n
		}
	}
}

// HasRole reports whether the process runs role r.
func (c Config) HasRole(r Role) bool {
	for _, x := range c.Roles {
		if x == r {
			return true
		}
	}
	return false
}

// Validate checks every key and returns all problems at once.
func (c Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if len(c.Roles) == 0 {
		add("roles: at least one of api, worker, scheduler is required")
	}
	for _, r := range c.Roles {
		if r != RoleAPI && r != RoleWorker && r != RoleScheduler {
			add("roles: unknown role %q", r)
		}
	}
	if c.Server.Listen == "" {
		add("server.listen: required")
	}
	if c.Server.PublicURL != "" {
		if u, err := url.Parse(c.Server.PublicURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			add("server.public_url: must be an absolute http(s) URL")
		}
	}
	if c.Email.SMTPURL() != "" && strings.TrimSpace(c.Email.From) == "" {
		add("email.from: required when %s is set (e.g. \"DocTheRepo <docs@example.com>\")", c.Email.SMTPURLEnv)
	}
	if s := c.Ask.SimilarAnswer; s != 0 && (s < 0.8 || s > 1) {
		add("ask.similar_answer: want 0 (off) or a similarity from 0.8 to 1, got %g", s)
	}
	switch c.Ask.Sift {
	case "", "on", "off":
	default:
		add("ask.sift: want on or off, got %q", c.Ask.Sift)
	}
	if k := c.Ask.SiftKeepAt; k != 0 && (k < 0.1 || k > 0.95) {
		add("ask.sift_keep_at: want a probability from 0.1 to 0.95, got %g", k)
	}
	if b := c.Spend.BufferPct; b < 0 || b > 50 || math.IsNaN(b) {
		add("spend.buffer_pct: want a percentage from 0 (off) to 50, got %g", b)
	}
	if c.Database.URLEnv == "" {
		add("database.url_env: required")
	}
	if c.Database.MaxConns < 2 {
		add("database.max_conns: must be >= 2 (the worker pool holds one listener connection)")
	}
	switch c.Secrets.Provider {
	case "localfile":
		if c.Secrets.LocalKeyFile == "" {
			add("secrets.local_key_file: required for provider localfile")
		}
	case "awskms", "gcpkms":
		if c.Secrets.KMSKeyID == "" {
			add("secrets.kms_key_id: required for provider %s", c.Secrets.Provider)
		}
	default:
		add("secrets.provider: must be localfile, awskms or gcpkms")
	}
	switch c.Auth.Mode {
	case "local":
	case "oidc":
		o := c.Auth.OIDC
		if o.Issuer == "" || o.ClientID == "" || o.RedirectURL == "" {
			add("auth.oidc: issuer, client_id and redirect_url are required when auth.mode is oidc")
		}
		if o.ClientSecretEnv == "" {
			add("auth.oidc.client_secret_env: required")
		}
	default:
		add("auth.mode: must be local or oidc")
	}
	if c.Auth.SessionIdle <= 0 || c.Auth.SessionAbsolute < c.Auth.SessionIdle {
		add("auth: session_idle must be > 0 and <= session_absolute")
	}
	for k, v := range c.Queue.Concurrency {
		if !knownJobType(k) {
			add("queue.concurrency: unknown job type %q", k)
		}
		if v < 0 { // 0 disables that job type on this node
			add("queue.concurrency.%s: must be >= 0", k)
		}
	}
	switch c.Vector.Backend {
	case "pgvector":
	case "qdrant":
		if c.Vector.QdrantURL == "" {
			add("vector.qdrant_url: required when vector.backend is qdrant")
		}
	default:
		add("vector.backend: must be pgvector or qdrant")
	}
	if c.Queue.LeaseTTL < 10*time.Second {
		add("queue.lease_ttl: must be >= 10s")
	}
	if c.Queue.MaxAttempts < 1 || c.Queue.MaxAttempts > 20 {
		add("queue.max_attempts: must be between 1 and 20")
	}
	if c.Queue.PollInterval <= 0 || c.Queue.BackoffBase <= 0 || c.Queue.BackoffMax < c.Queue.BackoffBase {
		add("queue: poll_interval and backoff_base must be > 0 and backoff_max >= backoff_base")
	}
	if c.Logging.Format != "json" && c.Logging.Format != "text" {
		add("logging.format: must be json or text")
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		add("logging.level: must be debug, info, warn or error")
	}
	if c.Tracing.Enabled && c.Tracing.OTLPEndpoint == "" {
		add("tracing.otlp_endpoint: required when tracing.enabled is true")
	}
	if c.Decide.GateThreshold < 0.5 || c.Decide.GateThreshold > 1 {
		add("decide.gate_threshold must be between 0.5 and 1 (got %v)", c.Decide.GateThreshold)
	}
	if err := ValidateDocsPath(c.Docs.DefaultPath); err != nil {
		add("docs.default_path: %v", err)
	}
	if c.Retention.EventDays < 1 || c.Retention.ChunkGCDays < 1 {
		add("retention: event_days and chunk_gc_days must be >= 1")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// ValidateDocsPath checks a generated-docs path: relative, clean, no parent traversal, not the repo root.
func ValidateDocsPath(p string) error {
	if p == "" {
		return errors.New("required")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return errors.New("must be relative to the repository root")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == "." || clean == "" {
		return errors.New("must not be the repository root")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return errors.New("must not contain '..'")
		}
	}
	return nil
}

func knownJobType(t string) bool {
	for _, jt := range ports.AllJobTypes {
		if string(jt) == t {
			return true
		}
	}
	return false
}
