package sigutil

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// GoogleClient authenticates with a service-account key JSON (a connector's credentials), or Application
// Default Credentials (Workload Identity) when none is set.
func GoogleClient(ctx context.Context, credentialsJSON string, scopes ...string) (*http.Client, error) {
	var ts oauth2.TokenSource
	if strings.TrimSpace(credentialsJSON) != "" {
		creds, err := google.CredentialsFromJSONWithType(ctx, []byte(credentialsJSON), google.ServiceAccount, scopes...)
		if err != nil {
			return nil, &ports.ValidationError{Code: "INVALID_CREDENTIALS", Message: "credentials must be a service-account key JSON"}
		}
		ts = creds.TokenSource
	} else {
		var err error
		if ts, err = google.DefaultTokenSource(ctx, scopes...); err != nil {
			return nil, fmt.Errorf("google credentials (Application Default Credentials): %w", err)
		}
	}
	hc := oauth2.NewClient(context.WithoutCancel(ctx), ts)
	hc.Timeout = 90 * time.Second
	return hc, nil
}
