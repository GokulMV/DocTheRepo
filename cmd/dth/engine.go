package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/engines/opencode"
)

// engineCmd groups doc-gen engines the Hub's external_cli provider can run (`command_template`).
func (a *app) engineCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "engine",
		Short: "Doc-generation engines for the external_cli provider",
	}
	var o opencode.Options
	oc := &cobra.Command{
		Use:   "opencode <task_file> <result_file>",
		Short: "Generate docs with opencode (DocGen contract v2)",
		Long: "Runs `opencode run --format json` for one DocGen task. Use it as an external_cli command template:\n\n" +
			"  dth engine opencode --model anthropic/claude-sonnet-5-5 {task_file} {result_file}\n\n" +
			"Exit codes: 0 success, 1 opencode failed (an error result is written), 3 unsupported contract version.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := opencode.Run(cmd.Context(), o, args[0], args[1])
			if errors.Is(err, opencode.ErrUnsupported) {
				fmt.Fprintln(a.errOut, "dth:", err)
				os.Exit(opencode.ExitUnsupported)
			}
			return err
		},
	}
	oc.Flags().StringVar(&o.Bin, "opencode", "", "opencode executable (default $OPENCODE_BIN or opencode on PATH)")
	oc.Flags().StringVar(&o.Model, "model", "", "provider/model for opencode (default: opencode's configured model)")
	oc.Flags().StringVar(&o.Agent, "agent", "", "opencode agent to use")
	oc.Flags().DurationVar(&o.Timeout, "timeout", 10*time.Minute, "maximum time for one opencode run")
	c.AddCommand(oc)
	return c
}
