package settings

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"golang.org/x/oauth2/google"
)

// awsSecret reads AWS Secrets Manager with the default credential chain (env, profile, instance role).
// An ARN carries its region; otherwise AWS_REGION / the profile's region applies.
func awsSecret(ctx context.Context, id string) (string, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("aws config: %w", err)
	}
	var opts []func(*secretsmanager.Options)
	if region := arnRegion(id); region != "" {
		opts = append(opts, func(o *secretsmanager.Options) { o.Region = region })
	}
	out, err := secretsmanager.NewFromConfig(cfg, opts...).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(id)})
	if err != nil {
		return "", fmt.Errorf("secrets manager: %w", err)
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	return string(out.SecretBinary), nil
}

// arnRegion returns the region of arn:aws:secretsmanager:REGION:…, or "".
func arnRegion(id string) string {
	var parts [6]string
	n := 0
	start := 0
	for i := 0; i < len(id) && n < 5; i++ {
		if id[i] == ':' {
			parts[n] = id[start:i]
			n++
			start = i + 1
		}
	}
	if n == 5 && parts[0] == "arn" && parts[2] == "secretsmanager" {
		return parts[3]
	}
	return ""
}

// gcpSecret reads Google Secret Manager over REST with Application Default Credentials.
func (r *Resolver) gcpSecret(ctx context.Context, name string) (string, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("google credentials: %w", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("google credentials: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://secretmanager.googleapis.com/v1/"+escapePath(name)+":access", nil)
	if err != nil {
		return "", err
	}
	tok.SetAuthHeader(req)
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("secret manager answered %d", resp.StatusCode)
	}
	var out struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(out.Payload.Data)
	if err != nil {
		return "", fmt.Errorf("secret manager payload: %w", err)
	}
	return string(b), nil
}
