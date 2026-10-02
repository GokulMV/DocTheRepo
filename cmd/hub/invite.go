package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// invite is the break-glass `dth-hub invite <email> [role]`: it prints a one-time password link straight
// from the database, for when nobody can sign in (single sign-on broke while passwords were off). Run it
// where the Hub runs (docker compose exec hub /dth-hub invite …, kubectl exec …), so the database URL and
// public URL come from the same environment.
func invite(args []string, cfgPath string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: dth-hub invite <email> [viewer|editor|admin|owner (default owner, for a new user)]")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.Open(ctx, store.Options{URL: cfg.Database.URL()})
	if err != nil {
		return err
	}
	defer st.Close()
	role := auth.RoleOwner
	if len(args) == 2 {
		if role, err = auth.ParseRole(args[1]); err != nil {
			return err
		}
	}
	svc := auth.New(st, cfg.Auth)
	actor := auth.SystemPrincipal("dth-hub invite")
	email := strings.ToLower(strings.TrimSpace(args[0]))
	var id string
	if u, err := st.Q.GetUserByEmail(ctx, email); err == nil {
		id = u.ID
	} else if store.IsNoRows(err) {
		nu, err := svc.CreateUser(ctx, actor, email, "", role)
		if err != nil {
			return err
		}
		id = nu.ID
		fmt.Fprintf(out, "Created %s as %s.\n", email, role)
	} else {
		return err
	}
	token, exp, err := svc.CreateInvite(ctx, actor, id)
	if err != nil {
		return err
	}
	_ = svc.Audit(ctx, actor, "user.invite", "user", id, map[string]string{"via": "dth-hub invite"}, "")
	base := strings.TrimRight(cfg.Server.PublicURL, "/")
	if base == "" {
		base = "<hub address>"
	}
	fmt.Fprintf(out, "Password link for %s (works once, until %s):\n%s/invite/%s\n", email, exp.Format("2006-01-02"), base, token)
	if !svc.PasswordEnabled(ctx) {
		fmt.Fprintln(out, "Password sign-in is off: restart the Hub with DTH_SETTINGS='auth: {password: true}' before using the link.")
	}
	return nil
}

func inviteMain() int {
	if err := invite(os.Args[2:], envOr("DTH_CONFIG", "dth.yaml"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dth-hub invite:", err)
		return 1
	}
	return 0
}
