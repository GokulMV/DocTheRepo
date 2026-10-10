package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/GokulMV/DocTheRepo/internal/adapters/awsrole"
)

// Header sets one header, e.g. Authorization: Bearer <token>, or api-key: <key>.
type Header struct{ Name, Value string }

// Authorize implements Authorizer.
func (h Header) Authorize(_ context.Context, req *http.Request, _ []byte) error {
	req.Header.Set(h.Name, h.Value)
	return nil
}

// Bearer is Authorization: Bearer <token>. A value that already names a scheme ("ApiKey …") is sent as is.
func Bearer(token string) Header {
	token = strings.TrimSpace(token)
	if i := strings.IndexByte(token, ' '); i > 0 && !strings.Contains(token[:i], ".") {
		return Header{Name: "Authorization", Value: token}
	}
	return Header{Name: "Authorization", Value: "Bearer " + token}
}

// TokenFunc fetches a bearer token for each request (OAuth access tokens, refreshed as they expire).
type TokenFunc func(ctx context.Context) (string, error)

// Authorize implements Authorizer.
func (f TokenFunc) Authorize(ctx context.Context, req *http.Request, _ []byte) error {
	t, err := f(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t)
	return nil
}

// AWSKeys are static AWS credentials, as stored sealed (JSON). Empty means the default chain: the IAM role
// of the machine, pod or task the Hub runs on, or the usual environment variables.
type AWSKeys struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
}

// newSTS builds the STS client that assumes the role (a fake in tests).
var newSTS = func(cfg aws.Config) stscreds.AssumeRoleAPIClient { return sts.NewFromConfig(cfg) }

// SigV4 signs requests for AWS-hosted MCP servers.
type SigV4 struct {
	Region, Service string
	creds           aws.CredentialsProvider
	signer          *v4.Signer
}

// NewSigV4 builds a signer from stored keys (JSON) or, when keys is empty, the default credential chain.
// With roleARN, those credentials only assume that role (with externalID when set) and requests are
// signed with the role's: the read-only role the Hub's CloudFormation template creates in another
// account. The session renews itself before it expires.
func NewSigV4(ctx context.Context, region, service, keys, roleARN, externalID string) (*SigV4, error) {
	if region == "" || service == "" {
		return nil, errors.New("AWS region and service are required")
	}
	s := &SigV4{Region: region, Service: service, signer: v4.NewSigner()}
	if strings.TrimSpace(keys) != "" {
		var k AWSKeys
		if err := json.Unmarshal([]byte(keys), &k); err != nil || k.AccessKeyID == "" || k.SecretAccessKey == "" {
			return nil, errors.New("AWS keys need an access key ID and a secret access key")
		}
		s.creds = credentials.NewStaticCredentialsProvider(k.AccessKeyID, k.SecretAccessKey, k.SessionToken)
	} else {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("find AWS credentials: %w", err)
		}
		s.creds = cfg.Credentials
	}
	if roleARN = strings.TrimSpace(roleARN); roleARN != "" {
		if !awsrole.ValidRoleARN(roleARN) {
			return nil, errors.New("the role ARN is not valid (arn:aws:iam::123456789012:role/Name)")
		}
		base := aws.Config{Region: region, Credentials: aws.NewCredentialsCache(s.creds)}
		s.creds = awsrole.Credentials(newSTS(base), roleARN, strings.TrimSpace(externalID))
		return s, nil
	}
	s.creds = aws.NewCredentialsCache(s.creds)
	return s, nil
}

// Authorize implements Authorizer.
func (s *SigV4) Authorize(ctx context.Context, req *http.Request, body []byte) error {
	c, err := s.creds.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("AWS credentials: %w", err)
	}
	sum := sha256.Sum256(body)
	return s.signer.SignHTTP(ctx, c, req, hex.EncodeToString(sum[:]), s.Service, s.Region, time.Now())
}

// GoogleScope is the scope Google Cloud's MCP servers accept.
const GoogleScope = "https://www.googleapis.com/auth/cloud-platform"

// NewGoogle authorizes with a service account key (JSON) or, when empty, Application Default Credentials
// (the attached service account on GKE, Cloud Run or Compute Engine).
func NewGoogle(ctx context.Context, serviceAccountJSON string) (TokenFunc, error) {
	var ts oauth2.TokenSource
	if strings.TrimSpace(serviceAccountJSON) != "" {
		c, err := google.CredentialsFromJSONWithType(ctx, []byte(serviceAccountJSON), google.ServiceAccount, GoogleScope)
		if err != nil {
			return nil, fmt.Errorf("read the service account key: %w", err)
		}
		ts = c.TokenSource
	} else {
		c, err := google.FindDefaultCredentials(ctx, GoogleScope)
		if err != nil {
			return nil, fmt.Errorf("find Google credentials: %w", err)
		}
		ts = c.TokenSource
	}
	ts = oauth2.ReuseTokenSource(nil, ts)
	return func(context.Context) (string, error) {
		t, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("Google token: %w", err)
		}
		return t.AccessToken, nil
	}, nil
}
