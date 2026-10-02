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
	c.AddCommand(export)
	return c
}
