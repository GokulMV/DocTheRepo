package awsrole

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const hubRole = "arn:aws:iam::111111111111:role/dth-hub"

func TestExternalIDAndNames(t *testing.T) {
	a, b := NewExternalID(), NewExternalID()
	assert.NotEqual(t, a, b)
	assert.Regexp(t, `^dth-[a-z2-7]{32}$`, a)
	assert.True(t, ValidExternalID(a))
	assert.False(t, ValidExternalID("has space"))
	assert.False(t, ValidExternalID("x"))
	assert.Equal(t, RolePrefix+a[4:14], RoleName(a))
	assert.Equal(t, StackPrefix+a[4:14], StackName(a))
	assert.LessOrEqual(t, len(RoleName(a)), 64, "IAM role names are at most 64 characters")
	assert.Empty(t, RoleName("someone-elses-id"))

	arn, err := RoleARN("aws", "222222222222", a)
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::222222222222:role/"+RoleName(a), arn)
	assert.True(t, ValidRoleARN(arn))
	assert.Equal(t, "222222222222", AccountOf(arn))
	_, err = RoleARN("aws", "2222", a)
	assert.Error(t, err)
	_, err = RoleARN("aws", "222222222222", "custom-id")
	assert.Error(t, err)
}

func TestPrincipalFromCaller(t *testing.T) {
	p, err := PrincipalFromCaller("arn:aws:sts::111111111111:assumed-role/dth-hub/i-0abc")
	require.NoError(t, err)
	assert.Equal(t, hubRole, p)
	p, err = PrincipalFromCaller("arn:aws-cn:sts::111111111111:assumed-role/hub/s")
	require.NoError(t, err)
	assert.Equal(t, "arn:aws-cn:iam::111111111111:role/hub", p)
	p, err = PrincipalFromCaller("arn:aws:iam::111111111111:user/ci")
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::111111111111:user/ci", p)
	_, err = PrincipalFromCaller("arn:aws:iam::111111111111:root")
	assert.ErrorContains(t, err, "root")
	_, err = PrincipalFromCaller("arn:aws:sts::111111111111:federated-user/bob")
	assert.Error(t, err)
}

// parse reads the template as plain YAML (long-form intrinsics need no custom tags).
func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(s), &m))
	return m
}

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = v.(map[string]any)[k]
		case int:
			v = v.([]any)[k]
		}
	}
	return v
}

func TestTemplate_TrustsOnlyTheHubWithTheExternalID(t *testing.T) {
	ext := NewExternalID()
	s, err := Template(TemplateParams{HubPrincipal: hubRole, ExternalID: ext, Access: AccessHub})
	require.NoError(t, err)
	m := parse(t, s)
	params := m["Parameters"].(map[string]any)
	assert.Equal(t, hubRole, dig(params, "HubPrincipalArn", "Default"))
	assert.Equal(t, ext, dig(params, "ExternalId", "Default"))
	assert.Equal(t, RoleName(ext), dig(params, "RoleName", "Default"))
	assert.Equal(t, "hub", dig(params, "AccessLevel", "Default"))

	role := dig(m, "Resources", "HubReadOnlyRole")
	assert.Equal(t, "AWS::IAM::Role", dig(role, "Type"))
	stmts := dig(role, "Properties", "AssumeRolePolicyDocument", "Statement").([]any)
	require.Len(t, stmts, 1, "one trust statement: nobody else is trusted")
	st := stmts[0]
	assert.Equal(t, "Allow", dig(st, "Effect"))
	assert.Equal(t, "sts:AssumeRole", dig(st, "Action"))
	assert.Equal(t, map[string]any{"Ref": "HubPrincipalArn"}, dig(st, "Principal", "AWS"))
	assert.Equal(t, map[string]any{"Ref": "ExternalId"}, dig(st, "Condition", "StringEquals", "sts:ExternalId"))
	assert.Equal(t, map[string]any{"Fn::GetAtt": []any{"HubReadOnlyRole", "Arn"}}, dig(m, "Outputs", "RoleArn", "Value"))

	// Only read calls: the narrow policy lists exactly HubActions, and the other branch is ReadOnlyAccess.
	inline := dig(role, "Properties", "Policies", "Fn::If", 2, 0, "PolicyDocument", "Statement").([]any)
	require.Len(t, inline, 1)
	assert.Equal(t, "Allow", dig(inline[0], "Effect"))
	var actions []string
	for _, a := range dig(inline[0], "Action").([]any) {
		actions = append(actions, a.(string))
		assert.Regexp(t, `^(cloudwatch|logs|sqs):(Describe|Get|List|Filter)`, a, "read-only verbs only")
	}
	assert.ElementsMatch(t, HubActions, actions)
	managed := dig(role, "Properties", "ManagedPolicyArns", "Fn::If", 1, 0, "Fn::Sub")
	assert.Equal(t, "arn:${AWS::Partition}:iam::aws:policy/ReadOnlyAccess", managed)
	assert.Equal(t, "FullReadOnly", dig(role, "Properties", "ManagedPolicyArns", "Fn::If", 0))
	for _, bad := range []string{"\"*\"", "- iam:", "Action: '*'", "Put", "Delete", "Create", "AdministratorAccess", "PowerUser"} {
		assert.NotContains(t, strings.SplitN(s, "Resources:", 2)[1], bad)
	}
}

func TestTemplate_GenericAndValidation(t *testing.T) {
	s, err := Template(TemplateParams{HubPrincipal: hubRole, Access: AccessReadOnly})
	require.NoError(t, err)
	params := parse(t, s)["Parameters"].(map[string]any)
	assert.NotContains(t, dig(params, "ExternalId").(map[string]any), "Default", "the generic template has no External ID: quick-create passes it")
	assert.Equal(t, "readonly", dig(params, "AccessLevel", "Default"))

	_, err = Template(TemplateParams{HubPrincipal: "arn:aws:iam::1:role/x'\nEvil: 1", Access: AccessHub})
	assert.Error(t, err)
	_, err = Template(TemplateParams{HubPrincipal: hubRole, ExternalID: "x'y", Access: AccessHub})
	assert.Error(t, err)
	_, err = Template(TemplateParams{HubPrincipal: hubRole, Access: "admin"})
	assert.Error(t, err)
}

func TestQuickCreateURLAndCommand(t *testing.T) {
	ext := NewExternalID()
	tu := "https://my-bucket.s3.eu-west-1.amazonaws.com/doctherepo-hub-readonly.yaml"
	link := QuickCreateURL(tu, "eu-west-1", hubRole, ext, AccessHub)
	base, frag, ok := strings.Cut(link, "#")
	require.True(t, ok)
	assert.Equal(t, "https://eu-west-1.console.aws.amazon.com/cloudformation/home?region=eu-west-1", base)
	path, query, _ := strings.Cut(frag, "?")
	assert.Equal(t, "/stacks/create/review", path)
	q, err := url.ParseQuery(query)
	require.NoError(t, err)
	assert.Equal(t, tu, q.Get("templateURL"))
	assert.Equal(t, StackName(ext), q.Get("stackName"))
	assert.Equal(t, hubRole, q.Get("param_HubPrincipalArn"))
	assert.Equal(t, ext, q.Get("param_ExternalId"))
	assert.Equal(t, RoleName(ext), q.Get("param_RoleName"))
	assert.Equal(t, "hub", q.Get("param_AccessLevel"))

	assert.Empty(t, QuickCreateURL("", "eu-west-1", hubRole, ext, AccessHub), "no S3 template: no link")
	assert.Empty(t, QuickCreateURL(tu, "", "arn:aws-cn:iam::111111111111:role/h", ext, AccessHub), "other partitions have other consoles")
	assert.Contains(t, QuickCreateURL(tu, "", hubRole, ext, AccessHub), "https://us-east-1.console.aws.amazon.com/")

	cmd := DeployCommand("eu-west-1", hubRole, ext, AccessReadOnly)
	assert.Equal(t, "aws cloudformation deploy --stack-name "+StackName(ext)+" --template-file "+StackName(ext)+".yaml --capabilities CAPABILITY_NAMED_IAM --region eu-west-1"+
		" --parameter-overrides HubPrincipalArn="+hubRole+" ExternalId="+ext+" RoleName="+RoleName(ext)+" AccessLevel=readonly", cmd)
}

// fakeSTS answers like STS: GetCallerIdentity for the Hub, and AssumeRole by a rule.
type fakeSTS struct {
	caller string
	assume func(in *sts.AssumeRoleInput) error
	calls  []*sts.AssumeRoleInput
}

func (f *fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.caller == "" {
		return nil, errors.New("no EC2 IMDS role found")
	}
	return &sts.GetCallerIdentityOutput{Arn: awssdk.String(f.caller), Account: awssdk.String("111111111111")}, nil
}

func (f *fakeSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.calls = append(f.calls, in)
	if err := f.assume(in); err != nil {
		return nil, err
	}
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{AccessKeyId: awssdk.String("ASIA"), SecretAccessKey: awssdk.String("s"),
		SessionToken: awssdk.String("t"), Expiration: awssdk.Time(time.Now().Add(15 * time.Minute))}}, nil
}

func apiErr(code, msg string) error { return &smithy.GenericAPIError{Code: code, Message: msg} }

// trusting assumes only with the right External ID, like the template's trust policy.
func trusting(ext string) func(*sts.AssumeRoleInput) error {
	return func(in *sts.AssumeRoleInput) error {
		if awssdk.ToString(in.ExternalId) != ext {
			return apiErr("AccessDenied", "User: arn:aws:sts::111111111111:assumed-role/dth-hub/x is not authorized to perform: sts:AssumeRole on resource: "+awssdk.ToString(in.RoleArn))
		}
		return nil
	}
}

func newHub(f *fakeSTS, probes func(awssdk.Config, []string) []Probe) *Hub {
	return &Hub{STS: func(context.Context) (STSAPI, error) { return f, nil }, Probes: probes}
}

func allowAll(_ awssdk.Config, actions []string) []Probe {
	var out []Probe
	for _, a := range actions {
		out = append(out, Probe{a, func(context.Context) error { return nil }})
	}
	return out
}

func TestCheck_Success(t *testing.T) {
	ext := NewExternalID()
	f := &fakeSTS{caller: "arn:aws:sts::111111111111:assumed-role/dth-hub/task", assume: trusting(ext)}
	var gotCfg awssdk.Config
	h := newHub(f, func(cfg awssdk.Config, a []string) []Probe { gotCfg = cfg; return allowAll(cfg, a) })
	r := h.Check(context.Background(), CheckInput{AccountID: "2222-2222-2222", ExternalID: ext, Uses: "cloudwatch", Region: "eu-west-1"})
	require.True(t, r.OK, r.Message)
	assert.Equal(t, "arn:aws:iam::222222222222:role/"+RoleName(ext), r.RoleARN)
	assert.Equal(t, "222222222222", r.AccountID)
	require.Len(t, f.calls, 2, "with the External ID, then without it")
	assert.Equal(t, ext, awssdk.ToString(f.calls[0].ExternalId))
	assert.Nil(t, f.calls[1].ExternalId)
	assert.Equal(t, "eu-west-1", gotCfg.Region)
	c, err := gotCfg.Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ASIA", c.AccessKeyID, "probes run as the role")
}

func TestCheck_Failures(t *testing.T) {
	ext := NewExternalID()
	caller := "arn:aws:sts::111111111111:assumed-role/dth-hub/task"
	cases := []struct {
		name    string
		assume  func(*sts.AssumeRoleInput) error
		probes  func(awssdk.Config, []string) []Probe
		problem string
		says    string
	}{
		{"no such role", func(*sts.AssumeRoleInput) error { return apiErr("NoSuchEntity", "role not found") }, nil, ProblemRoleNotFound, "CREATE_COMPLETE"},
		{"access denied without resource", func(*sts.AssumeRoleInput) error {
			return apiErr("AccessDenied", "Not authorized to perform sts:AssumeRole")
		}, nil, ProblemRoleNotFound, "check the account ID"},
		{"trust policy or external id", trusting("another-id"), nil, ProblemTrustDenied, "trust policy"},
		{"hub has no sts:AssumeRole", func(in *sts.AssumeRoleInput) error {
			return apiErr("AccessDenied", "User: x is not authorized to perform: sts:AssumeRole on resource: y because no identity-based policy allows the sts:AssumeRole action")
		}, nil, ProblemHubNotAllowed, "arn:aws:iam::*:role/DocTheRepoHubReadOnly-*"},
		{"external id not required", func(*sts.AssumeRoleInput) error { return nil }, nil, ProblemExternalIDOptional, "sts:ExternalId"},
		{"missing permission", trusting(ext), func(_ awssdk.Config, actions []string) []Probe {
			var out []Probe
			for _, a := range actions {
				err := apiErr("ResourceNotFoundException", "no group") // allowed: the call got past IAM
				if a == "cloudwatch:DescribeAlarmHistory" {
					err = apiErr("AccessDenied", "not authorized")
				}
				out = append(out, Probe{a, func(context.Context) error { return err }})
			}
			return out
		}, ProblemMissingPermission, "cloudwatch:DescribeAlarmHistory"},
		{"sts unreachable", func(*sts.AssumeRoleInput) error { return errors.New("dial tcp: timeout") }, nil, ProblemOther, "could not reach"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probes := tc.probes
			if probes == nil {
				probes = allowAll
			}
			r := newHub(&fakeSTS{caller: caller, assume: tc.assume}, probes).Check(context.Background(), CheckInput{AccountID: "222222222222", ExternalID: ext, Uses: "cloudwatch"})
			assert.False(t, r.OK)
			assert.Equal(t, tc.problem, r.Problem, r.Message)
			assert.Contains(t, r.Message, tc.says)
		})
	}
	if r := newHub(&fakeSTS{caller: caller, assume: trusting(ext)}, allowAll).Check(context.Background(), CheckInput{AccountID: "12", ExternalID: ext}); assert.False(t, r.OK) {
		assert.Equal(t, ProblemInput, r.Problem)
	}
	r := newHub(&fakeSTS{caller: caller, assume: trusting("custom")}, allowAll).Check(context.Background(),
		CheckInput{RoleARN: "arn:aws:iam::333333333333:role/MyRole", ExternalID: "custom", Uses: "mcp"})
	assert.True(t, r.OK, "a role ARN works with any External ID")
	assert.Equal(t, "333333333333", r.AccountID)
}

func TestNoHubIdentity(t *testing.T) {
	h := newHub(&fakeSTS{}, nil)
	_, err := h.NewSetup(context.Background(), AccessHub, "")
	assert.ErrorIs(t, err, ErrNoIdentity)
	r := h.Check(context.Background(), CheckInput{AccountID: "222222222222", ExternalID: NewExternalID()})
	assert.Equal(t, ProblemNoIdentity, r.Problem)
	assert.Contains(t, r.Message, "access keys")
}

func TestNewSetup(t *testing.T) {
	f := &fakeSTS{caller: "arn:aws:sts::111111111111:assumed-role/dth-hub/task"}
	h := newHub(f, nil)
	s, err := h.NewSetup(context.Background(), "", "eu-west-1")
	require.NoError(t, err)
	assert.Equal(t, hubRole, s.HubPrincipal)
	assert.Equal(t, AccessHub, s.Access)
	assert.Equal(t, HubActions, s.Permissions)
	assert.Empty(t, s.QuickCreateURL, "no S3 template configured")
	assert.Contains(t, s.DeployCommand, "ExternalId="+s.ExternalID)
	h.TemplateURL = "https://b.s3.amazonaws.com/t.yaml"
	s2, err := h.NewSetup(context.Background(), AccessReadOnly, "")
	require.NoError(t, err)
	assert.NotEqual(t, s.ExternalID, s2.ExternalID, "a new External ID per connection")
	assert.Contains(t, s2.QuickCreateURL, "param_ExternalId="+s2.ExternalID)
	assert.Equal(t, []string{"AWS managed policy ReadOnlyAccess"}, s2.Permissions)
	_, err = h.NewSetup(context.Background(), "admin", "")
	assert.Error(t, err)

	h.PrincipalARN = "arn:aws:iam::111111111111:role/path/dth-hub"
	p, err := h.Principal(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::111111111111:role/path/dth-hub", p, "the override wins (roles with a path)")
}
