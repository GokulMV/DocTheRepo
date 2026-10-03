package main

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// profileCmd lists and switches named hubs, so one CLI works against nonlive and production.
func (a *app) profileCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "profile",
		Short: "List named hubs (nonlive, production, …) and switch between them",
		Long: "Each profile is a hub URL and token saved by `dth login --profile NAME`. Commands use the current profile,\n" +
			"or the one named by --profile / DTH_PROFILE.",
		RunE: func(*cobra.Command, []string) error {
			cfg := loadConfig()
			if len(cfg.Profiles) == 0 {
				fmt.Fprintln(a.out, "No profiles yet. Add one: dth login --profile nonlive --server https://hub.nonlive.example --token …")
				return nil
			}
			names := make([]string, 0, len(cfg.Profiles))
			for n := range cfg.Profiles {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				mark := " "
				if n == cfg.Current {
					mark = "*"
				}
				fmt.Fprintf(a.out, "%s %-14s %s\n", mark, n, cfg.Profiles[n].Server)
			}
			return nil
		},
	}
	c.AddCommand(&cobra.Command{
		Use:   "use NAME",
		Short: "Make NAME the current profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadConfig()
			p, ok := cfg.Profiles[args[0]]
			if !ok {
				return fmt.Errorf("no profile %q (sign in with `dth login --profile %s --server … --token …`)", args[0], args[0])
			}
			cfg.Current = args[0]
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Now using %s (%s).\n", args[0], p.Server)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "remove NAME",
		Short: "Forget a profile and its token",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := loadConfig()
			if _, ok := cfg.Profiles[args[0]]; !ok {
				return fmt.Errorf("no profile %q", args[0])
			}
			delete(cfg.Profiles, args[0])
			if cfg.Current == args[0] {
				cfg.Current = ""
			}
			if err := saveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Removed %s. Revoke its token in that hub under Account.\n", args[0])
			return nil
		},
	})
	return c
}
