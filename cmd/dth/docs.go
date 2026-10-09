package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// docsCmd groups the Docs v2 commands: price, write, and report on a repository's documents.
func (a *app) docsCmd() *cobra.Command {
	c := &cobra.Command{Use: "docs", Short: "Estimate, write, and report on a repository's documents"}
	c.AddCommand(a.docsEstimateCmd(), a.docsWriteCmd(), a.docsReportCmd())
	return c
}

func (a *app) docsEstimateCmd() *cobra.Command {
	var full bool
	c := &cobra.Command{
		Use:   "estimate <owner/repo>",
		Short: "Price writing the documents (no model is called)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.repoID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			path := "/repos/" + id + "/docs/estimate"
			if full {
				path += "?full=true"
			}
			var out struct {
				Documents       int     `json:"documents"`
				Unchanged       int     `json:"unchanged"`
				Modules         int     `json:"modules"`
				EstimatedTokens int64   `json:"estimated_tokens"`
				EstimatedUSD    float64 `json:"estimated_usd"`
			}
			if err := a.client().call(cmd.Context(), "POST", path, nil, &out); err != nil {
				return err
			}
			if a.asJSON {
				return a.printJSON(out)
			}
			fmt.Fprintf(a.out, "Would write %d documents (%d unchanged, %d modules): ~%d tokens, ~$%.2f.\n",
				out.Documents, out.Unchanged, out.Modules, out.EstimatedTokens, out.EstimatedUSD)
			return nil
		},
	}
	c.Flags().BoolVar(&full, "full", false, "price rewriting every document, not only what is missing or changed")
	return c
}

func (a *app) docsWriteCmd() *cobra.Command {
	var full, wait bool
	var capUSD float64
	var only []string
	c := &cobra.Command{
		Use:   "write <owner/repo>",
		Short: "Write the documents (uses the configured model and counts against the repository's monthly cap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := a.repoID(ctx, args[0])
			if err != nil {
				return err
			}
			if capUSD > 0 {
				if err := a.client().call(ctx, "PUT", "/repos/"+id+"/docs/budget", map[string]any{"cap_usd": capUSD}, nil); err != nil {
					return err
				}
				fmt.Fprintf(a.out, "Monthly cap set to $%.2f.\n", capUSD)
			}
			var out struct {
				JobID string `json:"job_id"`
			}
			if err := a.client().call(ctx, "POST", "/repos/"+id+"/docs/write", map[string]any{"full": full, "only": only}, &out); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Writing: job %s.\n", out.JobID)
			if !wait {
				return nil
			}
			return a.waitJob(ctx, out.JobID, a.out)
		},
	}
	c.Flags().BoolVar(&full, "full", false, "rewrite every document, not only what is missing or changed")
	c.Flags().BoolVar(&wait, "wait", false, "wait until the job finishes, printing progress")
	c.Flags().Float64Var(&capUSD, "cap", 0, "set the repository's monthly docs cap (USD) first")
	c.Flags().StringSliceVar(&only, "only", nil, "write only these document types (e.g. overview,architecture)")
	return c
}

// waitJob polls a job until it stops, printing its progress when it changes.
func (a *app) waitJob(ctx context.Context, id string, w io.Writer) error {
	last := ""
	for {
		var j ports.Job
		if err := a.client().call(ctx, "GET", "/jobs/"+id, nil, &j); err != nil {
			return err
		}
		line := string(j.Status)
		if j.Progress != nil {
			line += " " + string(j.Progress)
		}
		if line != last {
			fmt.Fprintf(w, "%s  %s\n", time.Now().Format("15:04:05"), line)
			last = line
		}
		if j.Status.Terminal() {
			if j.Status != ports.JobDone {
				return fmt.Errorf("job %s ended %s: %s", id, j.Status, j.Error)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (a *app) docsReportCmd() *cobra.Command {
	var text bool
	var outPath string
	c := &cobra.Command{
		Use:   "report <owner/repo>",
		Short: "Report on the documents: cost, repairs, confidence, weak and off-length sections",
		Long: "Report on a repository's documents for review and prompt tuning. Without --text it holds no document prose, " +
			"only titles, scores, counts and the checks' findings (which can name files and identifiers from the code).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := a.repoID(ctx, args[0])
			if err != nil {
				return err
			}
			q := url.Values{}
			if text {
				q.Set("text", "true")
			}
			if !a.asJSON {
				q.Set("format", "md")
			}
			resp, err := a.client().request(ctx, "GET", "/repos/"+id+"/docs/report?"+q.Encode(), nil, "")
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			w := a.out
			if outPath != "" {
				f, err := os.Create(outPath)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			if _, err := io.Copy(w, resp.Body); err != nil {
				return err
			}
			if outPath != "" {
				fmt.Fprintf(a.out, "Report written to %s.\n", outPath)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&text, "text", false, "include the documents' text")
	c.Flags().StringVarP(&outPath, "out", "o", "", "write the report to a file")
	return c
}
