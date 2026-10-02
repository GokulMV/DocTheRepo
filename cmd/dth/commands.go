package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/externalcli"
	"github.com/GokulMV/DocTheRepo/internal/bootstrap"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/schema"
)

type app struct {
	out, errOut io.Writer
	server      string
	token       string
	asJSON      bool
	// runner overrides docker for `up`/`down` in tests.
	runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

func (a *app) client() *client { return newClient(a.server, a.token) }

// printJSON prints v as indented JSON.
func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *app) table(header string, rows [][]string) {
	w := tabwriter.NewWriter(a.out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, header)
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	w.Flush()
}

func newRoot(out, errOut io.Writer) *cobra.Command {
	a := &app{out: out, errOut: errOut}
	cfg := loadConfig()
	root := &cobra.Command{
		Use:           "dth",
		Short:         "DocTheRepo Hub CLI",
		Long:          "dth runs a local DocTheRepo Hub and talks to any hub: ask questions, manage repositories, jobs, usage, and tokens.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		PersistentPreRun: func(*cobra.Command, []string) {
			if a.server == "" {
				a.server = firstNonEmpty(os.Getenv("DTH_SERVER"), cfg.Server, "http://localhost:8080")
			}
			if a.token == "" {
				a.token = firstNonEmpty(os.Getenv("DTH_TOKEN"), cfg.Token)
			}
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().StringVar(&a.server, "server", "", "hub URL (env DTH_SERVER; default: saved by dth login, else http://localhost:8080)")
	root.PersistentFlags().StringVar(&a.token, "token", "", "personal access token (env DTH_TOKEN; default: saved by dth login)")
	root.PersistentFlags().BoolVar(&a.asJSON, "json", false, "print raw JSON")
	root.AddCommand(a.upCmd(), a.downCmd(), a.loginCmd(), a.statusCmd(), a.askCmd(), a.reposCmd(), a.importCmd(), a.dryRunCmd(),
		a.jobsCmd(), a.retryCmd(), a.usageCmd(), a.tokenCmd(), a.reindexCmd(), a.adapterTestCmd(), a.migrateCmd(), a.engineCmd(), a.mcpCmd(), a.applyCmd(), a.settingsCmd())
	return root
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- local stack ---

func (a *app) upCmd() *cobra.Command {
	var o bootstrap.Options
	var noBrowser bool
	c := &cobra.Command{
		Use:   "up",
		Short: "Start (or upgrade, or check) the local hub with Docker Compose",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Out, o.Runner = a.out, a.runner
			res, err := bootstrap.Up(cmd.Context(), o)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "\nDocTheRepo Hub is ready at %s\n", res.URL)
			if res.OwnerPassword != "" {
				fmt.Fprintf(a.out, "\nOwner account (shown once — save it now):\n  email:    %s\n  password: %s\n", res.OwnerEmail, res.OwnerPassword)
			}
			if res.Token != "" {
				if err := saveConfig(cliConfig{Server: res.URL, Token: res.Token}); err != nil {
					return err
				}
				fmt.Fprintln(a.out, "\nThe CLI is signed in (token saved to "+configPath()+").")
			}
			fmt.Fprintln(a.out, "\nNext: open the setup checklist to connect GitHub/GitLab, add your LLM provider, and pick repositories.")
			if !noBrowser {
				openBrowser(res.URL + "/setup")
			}
			return nil
		},
	}
	c.Flags().StringVar(&o.Dir, "dir", "", "state directory (default ~/.dth)")
	c.Flags().StringVar(&o.Image, "image", "", "hub image (default ghcr.io/gokulmv/doctherepo-hub:latest)")
	c.Flags().IntVar(&o.Port, "port", 8080, "local port")
	c.Flags().StringVar(&o.OwnerEmail, "owner-email", "", "owner account email on first run")
	c.Flags().BoolVar(&o.Ollama, "ollama", false, "also run Ollama for local models")
	c.Flags().BoolVar(&o.Pull, "upgrade", false, "pull newer images before starting")
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open the browser")
	return c
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

func (a *app) downCmd() *cobra.Command {
	var dir string
	var wipe bool
	c := &cobra.Command{
		Use:   "down",
		Short: "Stop the local hub (data is kept unless --wipe)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := bootstrap.Down(cmd.Context(), bootstrap.Options{Dir: dir, Out: a.out, Runner: a.runner}, wipe); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Stopped.")
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "state directory (default ~/.dth)")
	c.Flags().BoolVar(&wipe, "wipe", false, "also delete all data (database, keys)")
	return c
}

// --- remote hub ---

func (a *app) loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Save the hub URL and a personal access token (create one under Account in the UI)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.token == "" {
				return errors.New("pass --token (create one in the UI under Account → Personal access tokens)")
			}
			var me map[string]any
			if err := a.client().call(cmd.Context(), "GET", "/me", nil, &me); err != nil {
				return err
			}
			if err := saveConfig(cliConfig{Server: a.server, Token: a.token}); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Signed in to %s as %v (%v).\n", a.server, me["email"], me["role"])
			return nil
		},
	}
}

func (a *app) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Hub readiness, your identity, and the job queue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := a.client()
			var me map[string]any
			if err := c.call(ctx, "GET", "/me", nil, &me); err != nil {
				return err
			}
			var repos struct{ Items []ports.RepoConfig }
			if err := c.call(ctx, "GET", "/repos", nil, &repos); err != nil {
				return err
			}
			counts := map[string]int{}
			for _, st := range []string{"queued", "processing", "failed", "spend_blocked", "needs_human"} {
				var p struct{ Items []json.RawMessage }
				if err := c.call(ctx, "GET", "/jobs?limit=200&status="+st, nil, &p); err != nil {
					return err
				}
				counts[st] = len(p.Items)
			}
			if a.asJSON {
				return a.printJSON(map[string]any{"server": a.server, "me": me, "repos": len(repos.Items), "jobs": counts})
			}
			fmt.Fprintf(a.out, "Hub:    %s\nUser:   %v (%v)\nRepos:  %d tracked\nJobs:   %d queued, %d running, %d failed, %d spend-blocked, %d need a human\n",
				a.server, me["email"], me["role"], len(repos.Items), counts["queued"], counts["processing"], counts["failed"], counts["spend_blocked"], counts["needs_human"])
			return nil
		},
	}
}

func (a *app) repoID(ctx context.Context, name string) (string, error) {
	var repos struct{ Items []ports.RepoConfig }
	if err := a.client().call(ctx, "GET", "/repos", nil, &repos); err != nil {
		return "", err
	}
	for _, r := range repos.Items {
		if r.FullName == name || r.ID == name {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("no tracked repository %q (see `dth repos`)", name)
}

func (a *app) askCmd() *cobra.Command {
	var repos, include []string
	c := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask a question; the answer streams with citations",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var ids []string
			for _, r := range repos {
				id, err := a.repoID(ctx, r)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			body := map[string]any{"question": strings.Join(args, " "), "scope": map[string]any{"repo_ids": ids, "include": include}}
			if a.asJSON {
				var out map[string]any
				if err := a.client().call(ctx, "POST", "/ask", body, &out); err != nil {
					return err
				}
				return a.printJSON(out)
			}
			resp, err := a.client().request(ctx, "POST", "/ask", body, "text/event-stream")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			return renderStream(resp.Body, a.out)
		},
	}
	c.Flags().StringSliceVar(&repos, "repo", nil, "limit to repositories (owner/name), repeatable")
	c.Flags().StringSliceVar(&include, "include", nil, "sources: code, docs, confluence, issues")
	return c
}

func (a *app) reposCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "repos",
		Short: "List tracked repositories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var repos struct{ Items []ports.RepoConfig }
			if err := a.client().call(cmd.Context(), "GET", "/repos", nil, &repos); err != nil {
				return err
			}
			if a.asJSON {
				return a.printJSON(repos.Items)
			}
			rows := make([][]string, len(repos.Items))
			for i, r := range repos.Items {
				rows[i] = []string{r.FullName, r.Branch(), r.DocsPath, string(r.Push.Mode), short(r.LastProcessedSHA), fmt.Sprint(r.Enabled)}
			}
			a.table("REPOSITORY\tBRANCH\tDOCS PATH\tLANDING\tPROCESSED\tENABLED", rows)
			return nil
		},
	}
	var connector string
	add := &cobra.Command{
		Use:   "add <owner/name>",
		Short: "Track a repository on a git connector",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if connector == "" {
				return errors.New("--connector <id> is required (see the Connectors page)")
			}
			var out map[string]string
			if err := a.client().call(cmd.Context(), "POST", "/repos", map[string]string{"connector_id": connector, "full_name": args[0]}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Tracking %s (%s).\n", args[0], out["id"])
			return nil
		},
	}
	add.Flags().StringVar(&connector, "connector", "", "git connector ID")
	c.AddCommand(add)
	return c
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

func (a *app) importCmd() *cobra.Command {
	var paths []string
	c := &cobra.Command{
		Use:   "import <owner/name>",
		Short: "Import a repository's existing Markdown into the index and Library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.repoID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var out map[string]string
			if err := a.client().call(cmd.Context(), "POST", "/repos/"+id+"/import", map[string]any{"source_paths": paths}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Import queued: job %s\n", out["job_id"])
			return nil
		},
	}
	c.Flags().StringSliceVar(&paths, "path", nil, "only paths under these prefixes")
	return c
}

func (a *app) dryRunCmd() *cobra.Command {
	var before string
	c := &cobra.Command{
		Use:   "dry-run <owner/name>",
		Short: "Show what the next push would document and roughly what it costs (no writes, no spend)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.repoID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var out struct {
				Status, Message string
				Result          map[string]any
			}
			if err := a.client().call(cmd.Context(), "POST", "/repos/"+id+"/dry-run", map[string]string{"before": before}, &out); err != nil {
				return err
			}
			if a.asJSON {
				return a.printJSON(out)
			}
			fmt.Fprintf(a.out, "%s: %s\n", out.Status, out.Message)
			if t, ok := out.Result["triage"].(map[string]any); ok {
				fmt.Fprintf(a.out, "Triage: %v — %v\n", t["decision"], t["reason"])
			}
			fmt.Fprintf(a.out, "Chunks to document: %v, estimated input tokens: %v\n", out.Result["documented"], out.Result["estimated_tokens"])
			return nil
		},
	}
	c.Flags().StringVar(&before, "since", "", "compare from this commit (default: last processed)")
	return c
}

func (a *app) jobsCmd() *cobra.Command {
	var status, typ string
	c := &cobra.Command{
		Use:   "jobs",
		Short: "List recent jobs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{"limit": {"50"}}
			if status != "" {
				q.Set("status", status)
			}
			if typ != "" {
				q.Set("type", typ)
			}
			var p struct{ Items []ports.Job }
			if err := a.client().call(cmd.Context(), "GET", "/jobs?"+q.Encode(), nil, &p); err != nil {
				return err
			}
			if a.asJSON {
				return a.printJSON(p.Items)
			}
			rows := make([][]string, len(p.Items))
			for i, j := range p.Items {
				rows[i] = []string{j.ID, string(j.Type), string(j.Status), fmt.Sprintf("%d/%d", j.Attempts, j.MaxAttempts), j.UpdatedAt.Format(time.RFC3339), trunc(j.Error, 60)}
			}
			a.table("JOB\tTYPE\tSTATUS\tATTEMPTS\tUPDATED\tERROR", rows)
			return nil
		},
	}
	c.Flags().StringVar(&status, "status", "", "filter by status")
	c.Flags().StringVar(&typ, "type", "", "filter by type")
	return c
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func (a *app) retryCmd() *cobra.Command {
	var override bool
	c := &cobra.Command{
		Use:   "retry <job-id>",
		Short: "Replay a failed, dead, aborted, or spend-blocked job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out map[string]string
			if err := a.client().call(cmd.Context(), "POST", "/jobs/"+args[0]+"/retry", map[string]bool{"override_ceiling": override}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Queued job %s.\n", out["job_id"])
			return nil
		},
	}
	c.Flags().BoolVar(&override, "override-ceiling", false, "run a spend-blocked job once above the ceiling (audited)")
	return c
}

func (a *app) usageCmd() *cobra.Command {
	var by string
	var days int
	c := &cobra.Command{
		Use:   "usage",
		Short: "LLM usage and cost by feature, provider, model, repository, or user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(time.RFC3339)
			var rep usageReport
			if err := a.client().call(cmd.Context(), "GET", "/analytics/usage?"+url.Values{"group_by": {by}, "granularity": {"day"}, "from": {from}}.Encode(), nil, &rep); err != nil {
				return err
			}
			if a.asJSON {
				return a.printJSON(rep)
			}
			var rows [][]string
			for _, s := range rep.Series {
				var calls, tokens int64
				var cost float64
				for _, p := range s.Points {
					calls, tokens, cost = calls+p.Calls, tokens+p.Tokens, cost+p.CostUSD
				}
				rows = append(rows, []string{firstNonEmpty(s.Key, "-"), fmt.Sprint(calls), fmt.Sprint(tokens), fmt.Sprintf("$%.2f", cost)})
			}
			rows = append(rows, []string{"TOTAL", fmt.Sprint(rep.Totals.Calls), fmt.Sprint(rep.Totals.Tokens), fmt.Sprintf("$%.2f", rep.Totals.CostUSD)})
			a.table(strings.ToUpper(by)+"\tCALLS\tTOKENS\tCOST", rows)
			return nil
		},
	}
	c.Flags().StringVar(&by, "by", "feature", "feature, provider, model, repo, or user")
	c.Flags().IntVar(&days, "days", 7, "look-back window")
	return c
}

// usageReport mirrors the /analytics/usage response fields the CLI prints.
type usageReport struct {
	Series []struct {
		Key    string       `json:"key"`
		Points []usagePoint `json:"points"`
	} `json:"series"`
	Totals usagePoint `json:"totals"`
}

type usagePoint struct {
	Calls   int64   `json:"calls"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

func (a *app) tokenCmd() *cobra.Command {
	c := &cobra.Command{Use: "token", Short: "Manage your personal access tokens"}
	var days int
	create := &cobra.Command{
		Use:  "create <name>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out map[string]any
			if err := a.client().call(cmd.Context(), "POST", "/tokens", map[string]any{"name": args[0], "expires_in_days": days}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%v\n(shown once)\n", out["token"])
			return nil
		},
	}
	create.Flags().IntVar(&days, "expires-in-days", 90, "0 = never")
	list := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var out struct {
				Items []struct {
					ID, Name   string
					CreatedAt  time.Time  `json:"created_at"`
					LastUsedAt *time.Time `json:"last_used_at"`
				}
			}
			if err := a.client().call(cmd.Context(), "GET", "/tokens", nil, &out); err != nil {
				return err
			}
			rows := make([][]string, len(out.Items))
			for i, t := range out.Items {
				used := "never"
				if t.LastUsedAt != nil {
					used = t.LastUsedAt.Format(time.RFC3339)
				}
				rows[i] = []string{t.ID, t.Name, t.CreatedAt.Format(time.RFC3339), used}
			}
			a.table("ID\tNAME\tCREATED\tLAST USED", rows)
			return nil
		},
	}
	revoke := &cobra.Command{
		Use:  "revoke <id>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.client().call(cmd.Context(), "DELETE", "/tokens/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Revoked.")
			return nil
		},
	}
	c.AddCommand(create, list, revoke)
	return c
}

func (a *app) reindexCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "reindex",
		Short: "Re-embed every chunk on the embedding route's current model (owner only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				return errors.New("this re-embeds every chunk (paid calls on your provider); re-run with --yes")
			}
			var out map[string]any
			if err := a.client().call(cmd.Context(), "POST", "/reindex", map[string]bool{"acknowledge_destructive": true}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Reindex queued: job %v, %v chunks, ~%v tokens.\n", out["job_id"], out["chunks_to_reembed"], out["estimated_tokens"])
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm")
	return c
}

// adapterTestCmd runs the DocGen v2 conformance suite against a local engine command; no hub needed.
func (a *app) adapterTestCmd() *cobra.Command {
	var noUsage bool
	c := &cobra.Command{
		Use:   "adapter-test <command> [args...]",
		Short: "Check that an external doc-gen engine speaks the DocGen v2 contract",
		Long: "The engine command must take the task file and result file paths via the {task_file} and {result_file}\n" +
			"placeholders. Example:\n  dth adapter-test my-engine --task {task_file} --out {result_file}",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			quoted := make([]string, len(args))
			for i, arg := range args {
				quoted[i] = shellQuote(arg)
			}
			workDir, err := os.MkdirTemp("", "dth-adapter-test-")
			if err != nil {
				return fmt.Errorf("create work dir: %w", err)
			}
			defer os.RemoveAll(workDir)
			extra := map[string]string{"command_template": strings.Join(quoted, " "), "work_dir": workDir}
			if noUsage {
				extra["reports_usage"] = "false"
			}
			rep := externalcli.Conformance(cmd.Context(), ports.ProviderConfig{Kind: "external_cli", Extra: extra})
			if a.asJSON {
				return a.printJSON(rep)
			}
			for _, ch := range rep.Checks {
				mark := "PASS"
				if !ch.Passed {
					mark = "FAIL"
				}
				fmt.Fprintf(a.out, "%s  %s  %s\n", mark, ch.Name, ch.Detail)
			}
			if !rep.Passed {
				return errors.New("the engine does not conform to DocGen v2")
			}
			fmt.Fprintln(a.out, "Conforms to DocGen v2.")
			return nil
		},
	}
	c.Flags().BoolVar(&noUsage, "no-usage", false, "the engine does not report token usage")
	c.Flags().SetInterspersed(false) // flags after the command belong to the engine
	return c
}

// shellQuote quotes an argument for externalcli.SplitArgs (double quotes, backslash escapes).
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// migrateCmd applies or repairs schema migrations directly against the database (no hub needed).
func (a *app) migrateCmd() *cobra.Command {
	var dbURL string
	open := func(ctx context.Context) (*pgxpool.Pool, error) {
		u := firstNonEmpty(dbURL, os.Getenv("DTH_DATABASE_URL"))
		if u == "" {
			return nil, errors.New("--database-url or DTH_DATABASE_URL is required")
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		pool, err := pgxpool.New(cctx, u)
		if err != nil {
			return nil, fmt.Errorf("connect to database: %w", err)
		}
		if err := pool.Ping(cctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("ping database: %w", err)
		}
		return pool, nil
	}
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations (the hub also migrates on start)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			v, err := schema.Up(st)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Schema at version %d.\n", v)
			return nil
		},
	}
	c.PersistentFlags().StringVar(&dbURL, "database-url", "", "PostgreSQL URL (env DTH_DATABASE_URL)")
	c.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show the applied schema version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			v, err := schema.Version(st)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Schema at version %d.\n", v)
			return nil
		},
	}, &cobra.Command{
		Use:   "force <version>",
		Short: "Mark a version clean after fixing a failed migration by hand",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var n int
			if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil {
				return fmt.Errorf("version must be a number")
			}
			st, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer st.Close()
			v, err := schema.Force(st, n)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Forced schema version %d.\n", v)
			return nil
		},
	})
	return c
}
