// Package awsrole gives the Hub read-only access to someone's AWS account without access keys: a
// CloudFormation template (or quick-create link) makes a read-only IAM role in their account that trusts
// only the Hub's own IAM principal, and only with an External ID the Hub generated for that connection
// (the IAM guidance against the confused-deputy problem). The Hub then assumes the role with that ID,
// through a credentials cache that renews the session before it expires.
package awsrole

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"regexp"
	"slices"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
)

// Access levels for the role.
const (
	// AccessHub grants only the read calls the Hub's AWS features make (HubActions).
	AccessHub = "hub"
	// AccessReadOnly attaches the AWS managed policy ReadOnlyAccess: read access across services, for the
	// AWS MCP server, whose questions can touch any service.
	AccessReadOnly = "readonly"
)

// RolePrefix starts every role name the Hub asks for, so the Hub's own IAM policy can allow
// sts:AssumeRole on arn:aws:iam::*:role/DocTheRepoHubReadOnly-* and nothing else.
const RolePrefix = "DocTheRepoHubReadOnly-"

// StackPrefix starts the CloudFormation stack name.
const StackPrefix = "doctherepo-hub-readonly-"

// SessionName is the role session name the Hub uses (shown in the account's CloudTrail).
const SessionName = "doctherepo-hub"

// ReadOnlyPolicy is the managed policy behind AccessReadOnly, without its partition.
const ReadOnlyPolicy = "iam::aws:policy/ReadOnlyAccess"

// HubActions are the only API calls the Hub's AWS signal sources make (sts:GetCallerIdentity needs no
// permission). Each is read-only. SQS "peek" (sqs:ReceiveMessage) is left out on purpose: receiving
// hides a message for a moment and counts as a delivery, so it is not read-only; add it by hand if
// you turn peek on.
var HubActions = []string{
	"cloudwatch:DescribeAlarmHistory", // CloudWatch alarms (poll)
	"cloudwatch:GetMetricData",        // SNS, EventBridge, Kinesis, SQS metrics
	"cloudwatch:ListMetrics",          // finding topics, rules and streams
	"logs:FilterLogEvents",            // CloudWatch Logs (poll)
	"sqs:GetQueueAttributes",          // queue depth, oldest message, redrive policy
	"sqs:ListQueues",                  // finding queues
}

// Uses lists the actions each connector type (or the MCP connection) needs.
var Uses = map[string][]string{
	"cloudwatch":  {"logs:FilterLogEvents", "cloudwatch:DescribeAlarmHistory"},
	"sqs":         {"sqs:ListQueues", "sqs:GetQueueAttributes", "cloudwatch:GetMetricData"},
	"sns":         {"cloudwatch:ListMetrics", "cloudwatch:GetMetricData"},
	"eventbridge": {"cloudwatch:ListMetrics", "cloudwatch:GetMetricData"},
	"kinesis":     {"cloudwatch:ListMetrics", "cloudwatch:GetMetricData"},
	"mcp":         {},
}

var (
	hubExternalID = regexp.MustCompile(`^dth-([a-z2-7]{32})$`)
	anyExternalID = regexp.MustCompile(`^[\w+=,.@:/-]+$`) // the AssumeRole ExternalId characters
	accountID     = regexp.MustCompile(`^\d{12}$`)
	roleARN       = regexp.MustCompile(`^arn:(aws|aws-cn|aws-us-gov):iam::(\d{12}):role/[\w+=,.@/-]{1,512}$`)
	principalARN  = regexp.MustCompile(`^arn:(aws|aws-cn|aws-us-gov):iam::\d{12}:(role|user)/[\w+=,.@/-]{1,512}$`)
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewExternalID returns a fresh External ID: "dth-" and 160 random bits.
func NewExternalID() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return "dth-" + strings.ToLower(b32.EncodeToString(b))
}

// ValidExternalID reports whether s is an External ID AssumeRole accepts.
func ValidExternalID(s string) bool {
	return len(s) >= 2 && len(s) <= 1224 && anyExternalID.MatchString(s)
}

// ValidAccess reports whether a is an access level.
func ValidAccess(a string) bool { return a == AccessHub || a == AccessReadOnly }

// ValidRoleARN reports whether s is an IAM role ARN.
func ValidRoleARN(s string) bool { return roleARN.MatchString(s) }

// ValidPrincipalARN reports whether s is an IAM role or user ARN a trust policy can name.
func ValidPrincipalARN(s string) bool { return principalARN.MatchString(s) }

// suffix is the part of a Hub-made External ID that names the role and stack ("" for other IDs).
func suffix(externalID string) string {
	m := hubExternalID.FindStringSubmatch(externalID)
	if m == nil {
		return ""
	}
	return m[1][:10]
}

// RoleName is the role a Hub-made External ID asks for ("" for other IDs).
func RoleName(externalID string) string {
	if s := suffix(externalID); s != "" {
		return RolePrefix + s
	}
	return ""
}

// StackName is the CloudFormation stack for a Hub-made External ID.
func StackName(externalID string) string {
	if s := suffix(externalID); s != "" {
		return StackPrefix + s
	}
	return ""
}

// Partition reads the partition (aws, aws-cn, aws-us-gov) from an ARN.
func Partition(arn string) string {
	if p := strings.Split(arn, ":"); len(p) > 2 && p[0] == "arn" {
		return p[1]
	}
	return "aws"
}

// RoleARN is the role's ARN in account (a 12-digit ID) for a Hub-made External ID.
func RoleARN(partition, account, externalID string) (string, error) {
	if !accountID.MatchString(account) {
		return "", fmt.Errorf("an AWS account ID is 12 digits")
	}
	name := RoleName(externalID)
	if name == "" {
		return "", fmt.Errorf("this External ID was not made by the Hub; enter the role ARN instead of the account ID")
	}
	return "arn:" + partition + ":iam::" + account + ":role/" + name, nil
}

// AccountOf reads the account ID from a role ARN.
func AccountOf(arn string) string {
	if m := roleARN.FindStringSubmatch(arn); m != nil {
		return m[2]
	}
	return ""
}

// Permissions describes what a role at this access level may do, for the UI and audit log.
func Permissions(access string) []string {
	if access == AccessReadOnly {
		return []string{"AWS managed policy ReadOnlyAccess"}
	}
	return slices.Clone(HubActions)
}

// Credentials assumes roleARN with externalID (when set) through client (an STS client on the base
// credentials), behind a cache that renews the session shortly before it expires. Every AWS feature of
// the Hub uses it.
func Credentials(client stscreds.AssumeRoleAPIClient, roleARN, externalID string) awssdk.CredentialsProvider {
	return awssdk.NewCredentialsCache(stscreds.NewAssumeRoleProvider(client, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = SessionName
		if externalID != "" {
			o.ExternalID = awssdk.String(externalID)
		}
	}))
}
