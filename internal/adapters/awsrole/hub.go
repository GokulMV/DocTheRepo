package awsrole

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// STSAPI is the part of STS the Hub uses (the SDK client, or a fake in tests).
type STSAPI interface {
	GetCallerIdentity(ctx context.Context, in *sts.GetCallerIdentityInput, opts ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
	AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, opts ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

// Probe tries one read call with the assumed role's credentials; Run returns the call's error.
type Probe struct {
	Action string
	Run    func(ctx context.Context) error
}

// Hub knows the Hub's own AWS identity and checks roles made for it.
type Hub struct {
	// STS returns a client on the Hub's own credentials (default: the SDK's default chain: the IAM role
	// of the task, pod or instance, or AWS_* variables).
	STS func(ctx context.Context) (STSAPI, error)
	// PrincipalARN overrides the principal read from GetCallerIdentity (DTH_AWS_HUB_PRINCIPAL_ARN), for a
	// role with a path, which the session ARN does not show.
	PrincipalARN string
	// TemplateURL is the generic template in S3 (DTH_AWS_ROLE_TEMPLATE_URL); quick-create links need it.
	TemplateURL string
	// Probes lists read calls to try with the assumed role (default: real SDK calls).
	Probes func(cfg awssdk.Config, actions []string) []Probe

	mu        sync.Mutex
	principal string
}

// ErrNoIdentity: the Hub has no AWS credentials of its own (it does not run on AWS).
var ErrNoIdentity = errors.New("this Hub has no AWS identity of its own: it is not running on AWS with an IAM role, and no AWS credentials are set in its environment. Use access keys instead (or, for the AWS MCP server, sign in with AWS)")

var region = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d{1,2}$`)

// ValidRegion reports whether r looks like an AWS region (empty is allowed).
func ValidRegion(r string) bool { return r == "" || region.MatchString(r) }

func defaultSTS(ctx context.Context) (STSAPI, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1" // STS answers in every commercial region
	}
	return sts.NewFromConfig(cfg), nil
}

func (h *Hub) sts(ctx context.Context) (STSAPI, error) {
	if h.STS != nil {
		return h.STS(ctx)
	}
	return defaultSTS(ctx)
}

// Principal is the IAM role (or user) ARN the Hub runs as, which the role's trust policy names.
func (h *Hub) Principal(ctx context.Context) (string, error) {
	if h.PrincipalARN != "" {
		return h.PrincipalARN, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.principal != "" {
		return h.principal, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second) // off AWS, the metadata lookup gives up quickly
	defer cancel()
	c, err := h.sts(ctx)
	if err != nil {
		return "", ErrNoIdentity
	}
	out, err := c.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", ErrNoIdentity
	}
	p, err := PrincipalFromCaller(awssdk.ToString(out.Arn))
	if err != nil {
		return "", err
	}
	h.principal = p
	return p, nil
}

// PrincipalFromCaller turns GetCallerIdentity's ARN into one a trust policy can name: an assumed-role
// session becomes its role (arn:aws:sts::1:assumed-role/R/s → arn:aws:iam::1:role/R); a user stays.
func PrincipalFromCaller(arn string) (string, error) {
	p := strings.SplitN(arn, ":", 6)
	if len(p) != 6 || p[0] != "arn" {
		return "", fmt.Errorf("unexpected caller ARN %q", arn)
	}
	part, acct, res := p[1], p[4], p[5]
	switch {
	case p[2] == "sts" && strings.HasPrefix(res, "assumed-role/"):
		name := strings.SplitN(strings.TrimPrefix(res, "assumed-role/"), "/", 2)[0]
		return "arn:" + part + ":iam::" + acct + ":role/" + name, nil
	case p[2] == "iam" && strings.HasPrefix(res, "user/"):
		return arn, nil
	case p[2] == "iam" && res == "root":
		return "", errors.New("this Hub runs with the AWS account's root credentials; run it with an IAM role")
	}
	return "", fmt.Errorf("this Hub runs as %s, which a role's trust policy cannot name; run it with an IAM role or set DTH_AWS_HUB_PRINCIPAL_ARN", arn)
}

// Setup is everything the UI needs to create the role for one connection.
type Setup struct {
	HubPrincipal   string   `json:"hub_principal"`
	ExternalID     string   `json:"external_id"`
	Access         string   `json:"access"`
	RoleName       string   `json:"role_name"`
	StackName      string   `json:"stack_name"`
	Permissions    []string `json:"permissions"`
	QuickCreateURL string   `json:"quick_create_url,omitempty"`
	TemplateFile   string   `json:"template_file"`
	DeployCommand  string   `json:"deploy_command"`
}

// NewSetup makes a fresh External ID and the link or command that creates the role.
func (h *Hub) NewSetup(ctx context.Context, access, region string) (Setup, error) {
	if access == "" {
		access = AccessHub
	}
	if !ValidAccess(access) || !ValidRegion(region) {
		return Setup{}, fmt.Errorf("access must be %s or %s, and region an AWS region", AccessHub, AccessReadOnly)
	}
	p, err := h.Principal(ctx)
	if err != nil {
		return Setup{}, err
	}
	ext := NewExternalID()
	return Setup{HubPrincipal: p, ExternalID: ext, Access: access, RoleName: RoleName(ext), StackName: StackName(ext),
		Permissions: Permissions(access), QuickCreateURL: QuickCreateURL(h.TemplateURL, region, p, ext, access),
		TemplateFile: TemplateFile(ext), DeployCommand: DeployCommand(region, p, ext, access)}, nil
}

// Template renders the template with this Hub's principal (and the connection's values when set).
func (h *Hub) Template(ctx context.Context, externalID, access string) (string, error) {
	p, err := h.Principal(ctx)
	if err != nil {
		return "", err
	}
	return Template(TemplateParams{HubPrincipal: p, ExternalID: externalID, Access: access})
}

// CheckInput names the role to try: an account ID (with a Hub-made External ID) or a role ARN.
type CheckInput struct {
	AccountID  string `json:"account_id"`
	RoleARN    string `json:"role_arn"`
	ExternalID string `json:"external_id"`
	// Uses is the connector type ("cloudwatch", "sqs", …) or "mcp": which read calls to try.
	Uses   string `json:"uses"`
	Region string `json:"region"`
}

// Problems a check reports.
const (
	ProblemInput              = "invalid_input"
	ProblemNoIdentity         = "no_hub_identity"
	ProblemRoleNotFound       = "role_not_found"
	ProblemTrustDenied        = "trust_denied"
	ProblemHubNotAllowed      = "hub_not_allowed"
	ProblemExternalIDOptional = "external_id_not_required"
	ProblemMissingPermission  = "missing_permission"
	ProblemOther              = "error"
)

// CheckResult says whether the Hub can use the role, and if not, exactly why.
type CheckResult struct {
	OK        bool     `json:"ok"`
	RoleARN   string   `json:"role_arn,omitempty"`
	AccountID string   `json:"account_id,omitempty"`
	Problem   string   `json:"problem,omitempty"`
	Message   string   `json:"message"`
	Missing   []string `json:"missing,omitempty"`
}

func fail(r CheckResult, problem, msg string) CheckResult {
	r.Problem, r.Message = problem, msg
	return r
}

// Check assumes the role with the External ID, makes sure it cannot be assumed without one, and tries
// the read calls the connection will make.
func (h *Hub) Check(ctx context.Context, in CheckInput) CheckResult {
	var res CheckResult
	in.AccountID, in.RoleARN, in.ExternalID = strings.TrimSpace(in.AccountID), strings.TrimSpace(in.RoleARN), strings.TrimSpace(in.ExternalID)
	in.AccountID = strings.ReplaceAll(in.AccountID, "-", "") // the console shows 1234-5678-9012
	if !ValidExternalID(in.ExternalID) {
		return fail(res, ProblemInput, "the External ID is missing or not valid")
	}
	if !ValidRegion(in.Region) {
		return fail(res, ProblemInput, "not an AWS region")
	}
	principal, err := h.Principal(ctx)
	if err != nil {
		return fail(res, ProblemNoIdentity, err.Error())
	}
	role := in.RoleARN
	if role == "" {
		if role, err = RoleARN(Partition(principal), in.AccountID, in.ExternalID); err != nil {
			return fail(res, ProblemInput, err.Error())
		}
	} else if !ValidRoleARN(role) {
		return fail(res, ProblemInput, "a role ARN looks like arn:aws:iam::123456789012:role/Name")
	}
	res.RoleARN, res.AccountID = role, AccountOf(role)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := h.sts(ctx)
	if err != nil {
		return fail(res, ProblemNoIdentity, ErrNoIdentity.Error())
	}
	out, err := c.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: awssdk.String(role), RoleSessionName: awssdk.String(SessionName + "-check"),
		ExternalId: awssdk.String(in.ExternalID), DurationSeconds: awssdk.Int32(900)})
	if err != nil {
		p, msg := explainAssume(err, role, principal)
		return fail(res, p, msg)
	}
	// IAM's guidance: if the role can be assumed without the External ID, do not use it.
	if _, err := c.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: awssdk.String(role), RoleSessionName: awssdk.String(SessionName + "-check"),
		DurationSeconds: awssdk.Int32(900)}); err == nil {
		return fail(res, ProblemExternalIDOptional, "the role can be assumed without the External ID, so another customer of a shared Hub could use it too. Add the condition \"StringEquals\": {\"sts:ExternalId\": \""+in.ExternalID+"\"} to its trust policy (the Hub's template does this)")
	}
	cr := out.Credentials
	if cr == nil {
		return fail(res, ProblemOther, "STS returned no credentials")
	}
	reg := in.Region
	if reg == "" {
		reg = "us-east-1"
	}
	cfg := awssdk.Config{Region: reg, Credentials: credentials.NewStaticCredentialsProvider(awssdk.ToString(cr.AccessKeyId),
		awssdk.ToString(cr.SecretAccessKey), awssdk.ToString(cr.SessionToken))}
	probes := h.Probes
	if probes == nil {
		probes = sdkProbes
	}
	for _, p := range probes(cfg, Uses[in.Uses]) {
		if err := p.Run(ctx); err != nil && isDenied(err) {
			res.Missing = append(res.Missing, p.Action)
		}
	}
	if len(res.Missing) > 0 {
		return fail(res, ProblemMissingPermission, "the Hub can assume the role, but it is missing permission "+strings.Join(res.Missing, ", ")+
			". Recreate the stack with the Hub's template, or add the missing actions to the role")
	}
	res.OK, res.Message = true, "the Hub can use this role"
	return res
}

// explainAssume turns an AssumeRole failure into what to do. STS answers AccessDenied both for a role
// that does not exist and for one that does not trust the caller; only the second names the resource.
func explainAssume(err error, role, principal string) (string, string) {
	var ae smithy.APIError
	if !errors.As(err, &ae) {
		return ProblemOther, "could not reach AWS STS: " + err.Error()
	}
	msg := ae.ErrorMessage()
	switch ae.ErrorCode() {
	case "NoSuchEntity":
		return ProblemRoleNotFound, notFound(role)
	case "RegionDisabledException":
		return ProblemOther, "AWS STS is turned off in this region for the account: " + msg
	case "AccessDenied", "AccessDeniedException":
		switch {
		case strings.Contains(msg, "identity-based policy"):
			return ProblemHubNotAllowed, "the Hub's own IAM principal (" + principal + ") is not allowed to call sts:AssumeRole on " + role +
				". Add a policy to it allowing sts:AssumeRole on arn:" + Partition(principal) + ":iam::*:role/" + RolePrefix + "*"
		case strings.Contains(msg, "on resource"):
			return ProblemTrustDenied, "the role " + role + " exists but its trust policy does not let the Hub (" + principal +
				") in with this External ID. Create it with this connection's link or template, which set both"
		}
		return ProblemRoleNotFound, notFound(role)
	}
	return ProblemOther, ae.ErrorCode() + ": " + msg
}

func notFound(role string) string {
	return "the Hub found no role " + role + " it may assume. If the CloudFormation stack is still being created, wait until it shows CREATE_COMPLETE and check again; otherwise check the account ID"
}

// isDenied reports whether an API error means "not allowed" (rather than, say, "no such log group").
func isDenied(err error) bool {
	var ae smithy.APIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.ErrorCode() {
	case "AccessDenied", "AccessDeniedException", "AuthorizationError", "UnauthorizedOperation", "AccessDeniedFault":
		return true
	}
	return false
}

// sdkProbes are cheap read calls, one per action that can be tried without knowing any resource.
// logs:FilterLogEvents is tried on a log group that does not exist: "not found" means allowed.
func sdkProbes(cfg awssdk.Config, actions []string) []Probe {
	var out []Probe
	for _, a := range actions {
		switch a {
		case "logs:FilterLogEvents":
			out = append(out, Probe{a, func(ctx context.Context) error {
				_, err := cloudwatchlogs.NewFromConfig(cfg).FilterLogEvents(ctx, &cloudwatchlogs.FilterLogEventsInput{
					LogGroupName: awssdk.String("/doctherepo-hub/permission-check"), Limit: awssdk.Int32(1)})
				return err
			}})
		case "cloudwatch:DescribeAlarmHistory":
			out = append(out, Probe{a, func(ctx context.Context) error {
				_, err := cw.NewFromConfig(cfg).DescribeAlarmHistory(ctx, &cw.DescribeAlarmHistoryInput{MaxRecords: awssdk.Int32(1)})
				return err
			}})
		case "cloudwatch:ListMetrics":
			out = append(out, Probe{a, func(ctx context.Context) error {
				_, err := cw.NewFromConfig(cfg).ListMetrics(ctx, &cw.ListMetricsInput{Namespace: awssdk.String("AWS/SNS"), MetricName: awssdk.String("NumberOfNotificationsFailed")})
				return err
			}})
		case "sqs:ListQueues":
			out = append(out, Probe{a, func(ctx context.Context) error {
				_, err := sqs.NewFromConfig(cfg).ListQueues(ctx, &sqs.ListQueuesInput{MaxResults: awssdk.Int32(1)})
				return err
			}})
		}
	}
	return out
}
