package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/settings"
)

func (a *app) settingsAPI() (*settings.HTTPAPI, error) {
	if a.token == "" {
		return nil, errors.New("not signed in: run `dth login --server <url> --token <token>` (or `dth up` locally), or set DTH_TOKEN")
	}
	return &settings.HTTPAPI{Base: strings.TrimRight(a.server, "/") + "/api/v1", Header: http.Header{"Authorization": {"Bearer " + a.token}},
		Client: newClient(a.server, a.token).http}, nil
}

func (a *app) applyCmd() *cobra.Command {
	var files []string
	var dryRun bool
	c := &cobra.Command{
		Use:   "apply -f <file|dir> [-f …]",
		Short: "Apply a settings file: providers, routing, connectors, repositories, spend limits",
		Long: `apply makes the Hub match one or more YAML/JSON settings files (a directory means every .yaml/.yml/.json
in it; "-" reads stdin). Files are merged, so providers.yaml, repos.json and keys can live apart.

Secret values are references, resolved here on your machine or CI runner and sent to the Hub over its API:
  ${env:NAME}               environment variable (GitHub Actions: env: NAME: ${{ secrets.NAME }})
  ${file:path}              whole file;  ${file:keys.json#github.token}  a key in JSON/YAML
                            ${file:application.properties#db.password}   a key in .properties / .env
  ${vault:secret/data/dth#key}   HashiCorp Vault (VAULT_ADDR, VAULT_TOKEN or ~/.vault-token)
  ${gopass:path}  ${gopass:path#key}   gopass on PATH
  ${awssm:name-or-arn#key}  AWS Secrets Manager (default credentials)
  ${gcpsm:project/secret[/version]#key}   Google Secret Manager (application default credentials)
Write $${…} for a literal "${…}". apply creates and updates; it never deletes.`,
		Example: "  dth apply -f hub.yaml --dry-run\n  dth apply -f deploy/settings/\n  dth settings export > hub.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("name at least one settings file with -f")
			}
			srcs, err := settings.ReadPaths(files, cmd.InOrStdin())
			if err != nil {
				return err
			}
			r := &settings.Resolver{}
			doc, err := settings.Load(cmd.Context(), srcs, r)
			if err != nil {
				return err
			}
			api, err := a.settingsAPI()
			if err != nil {
				return err
			}
			res, err := settings.Apply(cmd.Context(), api, doc, dryRun)
			res.Sources = r.Used()
			if a.asJSON {
				if perr := a.printJSON(res); perr != nil {
					return perr
				}
				return err
			}
			a.printResult(res)
			return err
		},
	}
	c.Flags().StringArrayVarP(&files, "file", "f", nil, "settings file or directory (repeatable; - for stdin)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without changing anything")
	return c
}

func (a *app) printResult(res settings.Result) {
	rows := make([][]string, 0, len(res.Changes))
	for _, ch := range res.Changes {
		detail := strings.Join(ch.Fields, ", ")
		if ch.Detail != "" {
			detail = strings.TrimPrefix(detail+"; "+ch.Detail, "; ")
		}
		rows = append(rows, []string{ch.Action, ch.Kind, ch.Name, detail})
	}
	a.table("ACTION\tKIND\tNAME\tDETAIL", rows)
	create, update, unchanged := res.Counts()
	verb := "Applied"
	if res.DryRun {
		verb = "Would apply"
	}
	fmt.Fprintf(a.out, "\n%s: %d to create, %d to update, %d unchanged.", verb, create, update, unchanged)
	if len(res.Sources) > 0 {
		fmt.Fprintf(a.out, " Secrets from: %s.", strings.Join(res.Sources, ", "))
	}
	fmt.Fprintln(a.out)
	if res.DryRun && create+update > 0 {
		fmt.Fprintln(a.out, "Run again without --dry-run to apply.")
	}
}

func (a *app) settingsCmd() *cobra.Command {
	c := &cobra.Command{Use: "settings", Short: "Export the Hub's settings as a file for dth apply"}
	var out string
	export := &cobra.Command{
		Use:   "export",
		Short: "Write the current providers, routing, connectors, repositories and spend limits as YAML",
		Long:  "export writes the Hub's settings as YAML. Secrets cannot be read back: each one that is set becomes an ${env:…} reference to point at your secret store.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, err := a.settingsAPI()
			if err != nil {
				return err
			}
			doc, err := settings.Export(cmd.Context(), api)
			if err != nil {
				return err
			}
			b, err := settings.Marshal(doc)
			if err != nil {
				return err
			}
			if out == "" || out == "-" {
				_, err = a.out.Write(b)
				return err
			}
			return os.WriteFile(out, b, 0o600)
		},
	}
	export.Flags().StringVarP(&out, "output", "o", "", "file to write (default stdout)")
	var files []string
	resolve := &cobra.Command{
		Use:   "resolve",
		Short: "Print a settings file with its secret references filled in (for a secret store)",
		Long: "resolve reads settings files, replaces every ${…} reference with its value from your environment, files, " +
			"Vault, gopass or cloud secret managers, and prints the result. Use it to hand a deployment one secret (for " +
			"example Terraform's hub_settings or a Kubernetes Secret). The output contains secrets: never commit it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("pass one or more -f files")
			}
			srcs, err := settings.ReadPaths(files, cmd.InOrStdin())
			if err != nil {
				return err
			}
			doc, err := settings.Load(cmd.Context(), srcs, &settings.Resolver{})
			if err != nil {
				return err
			}
			b, err := settings.Marshal(doc)
			if err != nil {
				return err
			}
			_, err = a.out.Write(b)
			return err
		},
	}
	resolve.Flags().StringArrayVarP(&files, "file", "f", nil, "settings file or directory (repeatable; - for stdin)")
	c.AddCommand(export, resolve, a.settingsCheckCmd())
	return c
}

func (a *app) settingsCheckCmd() *cobra.Command {
	var files []string
	var resolveRefs, requireOwner bool
	c := &cobra.Command{
		Use:   "check -f <file|dir> [-f …]",
		Short: "Validate settings files before deploying, without a Hub",
		Long: `check reads settings files the way the Hub does at startup and reports problems without contacting a Hub:
syntax, unknown keys, missing fields, owners and admins outside the SSO allowed domains, and (with
--require-owner) a file that names no owner. With --resolve it also fetches every secret reference to prove
it exists, printing names only, never values. It exits non-zero on any problem, so a deploy pipeline can
run it first.`,
		Example: "  dth settings check -f deploy/environments/production/settings.yaml --require-owner\n" +
			"  dth settings check -f deploy/environments/production/ --require-owner --resolve",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("pass one or more -f files")
			}
			srcs, err := settings.ReadPaths(files, cmd.InOrStdin())
			if err != nil {
				return err
			}
			opts := settings.CheckOptions{RequireOwner: requireOwner}
			if resolveRefs {
				opts.Resolver = &settings.Resolver{}
			}
			rep, err := settings.Check(cmd.Context(), srcs, opts)
			if err != nil {
				return err
			}
			if a.asJSON {
				if err := a.printJSON(rep); err != nil {
					return err
				}
			} else {
				a.printCheck(rep)
			}
			if !rep.OK() {
				return fmt.Errorf("%d problem(s) in the settings", len(rep.Problems))
			}
			return nil
		},
	}
	c.Flags().StringArrayVarP(&files, "file", "f", nil, "settings file or directory (repeatable; - for stdin)")
	c.Flags().BoolVar(&resolveRefs, "resolve", false, "also resolve every secret reference (needs access to the secret stores)")
	c.Flags().BoolVar(&requireOwner, "require-owner", false, "fail unless at least one user has role owner")
	return c
}

func (a *app) printCheck(rep settings.CheckReport) {
	list := func(v []string) string {
		if len(v) == 0 {
			return "none"
		}
		return strings.Join(v, ", ")
	}
	sso := "not in this file (set by the deployment, or off)"
	if rep.SSO {
		sso = "configured"
	}
	password := "deployment default"
	if rep.Password != nil {
		password = map[bool]string{true: "on", false: "off"}[*rep.Password]
	}
	fmt.Fprintf(a.out, "Owners:    %s\nAdmins:    %s\nSSO:       %s\nPasswords: %s\n", list(rep.Owners), list(rep.Admins), sso, password)
	state := "not resolved (add --resolve)"
	if rep.Resolved {
		state = "resolved"
	}
	fmt.Fprintf(a.out, "Secrets:   %d reference(s), %s\n", len(rep.References), state)
	for _, r := range rep.References {
		fmt.Fprintf(a.out, "  %s\n", r)
	}
	if rep.OK() {
		fmt.Fprintln(a.out, "\nNo problems found.")
		return
	}
	fmt.Fprintln(a.out, "\nProblems:")
	for _, p := range rep.Problems {
		fmt.Fprintf(a.out, "  - %s\n", p)
	}
}
