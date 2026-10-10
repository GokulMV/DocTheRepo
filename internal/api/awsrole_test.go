package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/awsrole"
	"github.com/GokulMV/DocTheRepo/internal/api"
)

// roleSTS is a fake STS: the Hub runs as role dth-hub; AssumeRole succeeds only with externalID.
type roleSTS struct {
	caller     string
	externalID string
}

func (f *roleSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.caller == "" {
		return nil, errors.New("no credentials")
	}
	return &sts.GetCallerIdentityOutput{Arn: awssdk.String(f.caller)}, nil
}

func (f *roleSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	if !strings.HasPrefix(awssdk.ToString(in.RoleArn), "arn:aws:iam::222222222222:role/") {
		return nil, &smithy.GenericAPIError{Code: "AccessDenied", Message: "Not authorized to perform sts:AssumeRole"}
	}
	if f.externalID == "" || awssdk.ToString(in.ExternalId) != f.externalID {
		return nil, &smithy.GenericAPIError{Code: "AccessDenied", Message: "User: x is not authorized to perform: sts:AssumeRole on resource: " + awssdk.ToString(in.RoleArn)}
	}
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{AccessKeyId: awssdk.String("ASIA"), SecretAccessKey: awssdk.String("s"),
		SessionToken: awssdk.String("t"), Expiration: awssdk.Time(time.Now().Add(time.Hour))}}, nil
}

func awsRoleEnv(t *testing.T, f *roleSTS) (*authEnv, *client) {
	t.Helper()
	hub := &awsrole.Hub{STS: func(context.Context) (awsrole.STSAPI, error) { return f, nil },
		Probes: func(awssdk.Config, []string) []awsrole.Probe { return nil }}
	e := newAuthEnv(t, "local", true, func(d *api.Deps) {
		d.V1 = []func(chi.Router){api.AWSRoleRoutes(api.AWSRoleDeps{Auth: d.Auth, Hub: hub})}
	})
	_, err := e.svc.BootstrapOwner(context.Background(), "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	return e, e.localLogin(t, "owner@acme.com", "correct horse battery staple")
}

func TestAWSRole_SetupTemplateAndCheck(t *testing.T) {
	f := &roleSTS{caller: "arn:aws:sts::111111111111:assumed-role/dth-hub/task"}
	e, owner := awsRoleEnv(t, f)

	code, out, _ := owner.do("POST", "/api/v1/aws/role/setup", map[string]any{"access": "hub", "region": "eu-west-1"})
	require.Equal(t, http.StatusOK, code, out)
	require.Equal(t, true, out["available"], out)
	setup := out["setup"].(map[string]any)
	ext := setup["external_id"].(string)
	assert.Equal(t, "arn:aws:iam::111111111111:role/dth-hub", setup["hub_principal"])
	assert.Equal(t, awsrole.RoleName(ext), setup["role_name"])
	assert.Contains(t, setup["deploy_command"], "ExternalId="+ext)
	assert.Nil(t, setup["quick_create_url"], "no S3 template configured")

	// The template downloads with this connection's values and no secrets.
	req, _ := http.NewRequest("GET", e.srv.URL+out["template_path"].(string), nil)
	resp, err := owner.http.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Contains(t, resp.Header.Get("Content-Disposition"), awsrole.StackName(ext)+".yaml")
	assert.Contains(t, string(body), "Default: '"+ext+"'")
	assert.Contains(t, string(body), "sts:ExternalId:")
	code, _, _ = owner.do("GET", "/api/v1/aws/role/template?external_id=evil'", nil)
	assert.Equal(t, http.StatusBadRequest, code)

	// Not created yet, then created with the External ID.
	code, out, _ = owner.do("POST", "/api/v1/aws/role/check", map[string]any{"account_id": "222222222222", "external_id": ext, "uses": "sqs"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["ok"])
	assert.Equal(t, awsrole.ProblemTrustDenied, out["problem"], out)
	f.externalID = ext
	code, out, _ = owner.do("POST", "/api/v1/aws/role/check", map[string]any{"account_id": "222222222222", "external_id": ext, "uses": "sqs"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, true, out["ok"], out)
	assert.Equal(t, "arn:aws:iam::222222222222:role/"+awsrole.RoleName(ext), out["role_arn"])
	code, out, _ = owner.do("POST", "/api/v1/aws/role/check", map[string]any{"account_id": "333333333333", "external_id": ext})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, awsrole.ProblemRoleNotFound, out["problem"])
	code, _, _ = owner.do("POST", "/api/v1/aws/role/check", map[string]any{"account_id": "222222222222", "external_id": ext, "uses": "ec2"})
	assert.Equal(t, http.StatusBadRequest, code)
	code, _, _ = owner.do("POST", "/api/v1/aws/role/setup", map[string]any{"access": "admin"})
	assert.Equal(t, http.StatusBadRequest, code)

	// Audited.
	var n int
	require.NoError(t, e.st.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE action LIKE 'aws_role.%'`).Scan(&n))
	assert.GreaterOrEqual(t, n, 4)

	// CSRF: a cookie session without the header is refused.
	noCSRF := *owner
	noCSRF.csrf = ""
	code, out, _ = noCSRF.do("POST", "/api/v1/aws/role/setup", map[string]any{})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "CSRF_FAILED", errCode(out))

	// RBAC: admins only.
	code, out, _ = owner.do("POST", "/api/v1/users", map[string]any{"email": "ed@acme.com", "role": "editor", "invite": true})
	require.Equal(t, http.StatusCreated, code, out)
	token := strings.TrimPrefix(out["invite"].(map[string]any)["path"].(string), "/invite/")
	ed := newClient(t, e.srv.URL)
	code, out, _ = ed.do("POST", "/api/v1/auth/invite/"+token, map[string]string{"password": "eds long passphrase"})
	require.Equal(t, http.StatusOK, code, out)
	ed = e.localLogin(t, "ed@acme.com", "eds long passphrase")
	code, _, _ = ed.do("POST", "/api/v1/aws/role/setup", map[string]any{})
	assert.Equal(t, http.StatusForbidden, code)
	code, _, _ = ed.do("GET", "/api/v1/aws/role/template", nil)
	assert.Equal(t, http.StatusForbidden, code)
}

func TestAWSRole_NoHubIdentity(t *testing.T) {
	_, owner := awsRoleEnv(t, &roleSTS{})
	code, out, _ := owner.do("POST", "/api/v1/aws/role/setup", map[string]any{})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["available"])
	assert.Contains(t, out["reason"], "no AWS identity")
	code, out, _ = owner.do("GET", "/api/v1/aws/role/template", nil)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "NO_AWS_IDENTITY", errCode(out))
}
