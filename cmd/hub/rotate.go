package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// sealedColumns are every column holding a secret sealed by the Box.
var sealedColumns = []struct{ table, id, col, what string }{
	{"llm_providers", "id", "key_ciphertext", "model provider keys"},
	{"connectors", "id", "creds_ciphertext", "connector credentials"},
	{"connectors", "id", "webhook_secret_ct", "webhook secrets"},
	{"seal_keys", "id", "private_ct", "sealing keys"},
	{"auth_settings", "id", "oidc_secret_ciphertext", "single sign-on client secret"},
	{"mcp_servers", "id", "secret_ciphertext", "MCP connection keys"},
	{"mcp_servers", "id", "oauth_ciphertext", "MCP sign-in tokens"},
}

// rotateKey is `dth-hub rotate-key`: it re-wraps every stored secret's data key under a new
// key-encryption key, in one transaction. The secrets themselves are never decrypted. It also moves a
// deployment between providers (a local key file to AWS or Google Cloud KMS, or back).
func rotateKey(args []string, cfgPath string, out io.Writer) error {
	fs := flag.NewFlagSet("rotate-key", flag.ContinueOnError)
	fs.SetOutput(out)
	toFile := fs.String("to-key-file", "", "new local key file (created, 0600, if it does not exist)")
	toAWS := fs.String("to-awskms", "", "new AWS KMS key ID or ARN")
	toGCP := fs.String("to-gcpkms", "", "new Google Cloud KMS key name")
	dry := fs.Bool("dry-run", false, "count what would move without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	newB64 := os.Getenv("DTH_NEW_LOCAL_KEY")
	var to config.SecretsConfig
	n := 0
	for _, set := range []bool{*toFile != "", *toAWS != "", *toGCP != "", newB64 != ""} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("usage: dth-hub rotate-key [--dry-run] (--to-key-file PATH | --to-awskms KEY | --to-gcpkms KEY), or DTH_NEW_LOCAL_KEY=<base64 32 bytes>")
	}
	switch {
	case *toAWS != "":
		to = config.SecretsConfig{Provider: "awskms", KMSKeyID: *toAWS}
	case *toGCP != "":
		to = config.SecretsConfig{Provider: "gcpkms", KMSKeyID: *toGCP}
	default:
		to = config.SecretsConfig{Provider: "localfile", LocalKeyFile: *toFile}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	from, err := openKEK(ctx, cfg.Secrets, os.Getenv("DTH_LOCAL_KEY"))
	if err != nil {
		return fmt.Errorf("current key: %w", err)
	}
	next, err := openKEK(ctx, to, newB64)
	if err != nil {
		return fmt.Errorf("new key: %w", err)
	}
	if from.KeyID() == next.KeyID() {
		return fmt.Errorf("the new key is the current key (%s)", from.KeyID())
	}
	st, err := store.Open(ctx, store.Options{URL: cfg.Database.URL()})
	if err != nil {
		return err
	}
	defer st.Close()
	moved, err := rewrapAll(ctx, st, from, next, *dry)
	if err != nil {
		return err
	}
	verb := "Moved"
	if *dry {
		verb = "Would move"
	}
	total := 0
	for _, c := range sealedColumns {
		if moved[c.what] > 0 {
			fmt.Fprintf(out, "%s %d %s.\n", verb, moved[c.what], c.what)
			total += moved[c.what]
		}
	}
	fmt.Fprintf(out, "%s %d secrets from %s to %s.\n", verb, total, from.KeyID(), next.KeyID())
	if *dry {
		return nil
	}
	fmt.Fprintln(out, "Next: point the Hub at the new key and restart every Hub process:")
	switch to.Provider {
	case "localfile":
		if newB64 != "" {
			fmt.Fprintln(out, "  DTH_LOCAL_KEY=<the new key>")
		} else {
			fmt.Fprintf(out, "  DTH_SECRETS_PROVIDER=localfile DTH_LOCAL_KEY_FILE=%s\n", to.LocalKeyFile)
		}
	default:
		fmt.Fprintf(out, "  DTH_SECRETS_PROVIDER=%s DTH_KMS_KEY_ID=%s\n", to.Provider, to.KMSKeyID)
	}
	fmt.Fprintln(out, "Keep the old key until the Hub starts cleanly, then destroy it. Running the command again is safe.")
	return nil
}

// rewrapAll moves every sealed column to next in one transaction; already-moved secrets are skipped.
func rewrapAll(ctx context.Context, st *store.Store, from, next ports.KeyEncrypter, dry bool) (map[string]int, error) {
	moved := map[string]int{}
	err := st.InTx(ctx, func(_ *gen.Queries, tx pgx.Tx) error {
		for _, c := range sealedColumns {
			rows, err := tx.Query(ctx, fmt.Sprintf("SELECT %s::text, %s FROM %s WHERE %s IS NOT NULL FOR UPDATE", c.id, c.col, c.table, c.col))
			if err != nil {
				return fmt.Errorf("read %s.%s: %w", c.table, c.col, err)
			}
			type item struct {
				id   string
				blob []byte
			}
			var items []item
			for rows.Next() {
				var it item
				if err := rows.Scan(&it.id, &it.blob); err != nil {
					rows.Close()
					return err
				}
				items = append(items, it)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, it := range items {
				nb, changed, err := secrets.Rewrap(ctx, it.blob, from, next)
				if err != nil {
					return fmt.Errorf("%s %s: %w", c.table, it.id, err)
				}
				if !changed {
					continue
				}
				moved[c.what]++
				if dry {
					continue
				}
				if _, err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s::text = $2", c.table, c.col, c.id), nb, it.id); err != nil {
					return fmt.Errorf("update %s %s: %w", c.table, it.id, err)
				}
			}
		}
		return nil
	})
	return moved, err
}

func rotateKeyMain() int {
	if err := rotateKey(os.Args[2:], envOr("DTH_CONFIG", "dth.yaml"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "dth-hub rotate-key:", err)
		return 1
	}
	return 0
}
