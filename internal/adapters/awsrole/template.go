package awsrole

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"text/template"
)

// TemplateParams fill the template's parameter defaults. Empty ExternalID makes the generic template
// (for hosting in S3 behind quick-create links, which pass every value as param_*).
type TemplateParams struct {
	HubPrincipal string
	ExternalID   string
	Access       string
}

// Long-form intrinsic functions (Ref:, Fn::If:) keep the file plain YAML that any parser reads.
var tmpl = template.Must(template.New("role").Parse(`AWSTemplateFormatVersion: '2010-09-09'
Description: >-
  Read-only IAM role for the DocTheRepo Hub. Only the Hub's own IAM principal can assume it, and only
  with the External ID below. It can read, never change, resources in this account.
Parameters:
  HubPrincipalArn:
    Type: String
    Description: The Hub's IAM role (or user). The only principal the role trusts.
    AllowedPattern: '^arn:aws[a-z-]*:iam::\d{12}:(role|user)/.+$'
{{- if .HubPrincipal }}
    Default: '{{ .HubPrincipal }}'
{{- end }}
  ExternalId:
    Type: String
    Description: Made by the Hub for this connection. The Hub must present it to assume the role.
    MinLength: 2
    MaxLength: 1224
    AllowedPattern: '^[\w+=,.@:/-]+$'
{{- if .ExternalID }}
    Default: '{{ .ExternalID }}'
{{- end }}
  RoleName:
    Type: String
    Description: Name of the role. The Hub finds it by this name.
    AllowedPattern: '^{{ .RolePrefix }}[\w+=,.@-]{1,40}$'
{{- if .RoleName }}
    Default: '{{ .RoleName }}'
{{- end }}
  AccessLevel:
    Type: String
    Description: hub = only the read calls the Hub makes; readonly = the AWS managed policy ReadOnlyAccess.
    AllowedValues:
      - hub
      - readonly
    Default: '{{ .Access }}'
Conditions:
  FullReadOnly:
    Fn::Equals:
      - Ref: AccessLevel
      - readonly
Resources:
  HubReadOnlyRole:
    Type: AWS::IAM::Role
    Properties:
      RoleName:
        Ref: RoleName
      Description: Read-only access for the DocTheRepo Hub
      MaxSessionDuration: 3600
      AssumeRolePolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              AWS:
                Ref: HubPrincipalArn
            Action: sts:AssumeRole
            Condition:
              StringEquals:
                sts:ExternalId:
                  Ref: ExternalId
      ManagedPolicyArns:
        Fn::If:
          - FullReadOnly
          - - Fn::Sub: 'arn:${AWS::Partition}:{{ .ReadOnlyPolicy }}'
          - Ref: AWS::NoValue
      Policies:
        Fn::If:
          - FullReadOnly
          - Ref: AWS::NoValue
          - - PolicyName: DocTheRepoHubRead
              PolicyDocument:
                Version: '2012-10-17'
                Statement:
                  - Sid: HubReads
                    Effect: Allow
                    Action:
{{- range .Actions }}
                      - {{ . }}
{{- end }}
                    Resource: '*'
Outputs:
  RoleArn:
    Description: Paste this (or just the account ID) into the Hub.
    Value:
      Fn::GetAtt:
        - HubReadOnlyRole
        - Arn
  ExternalId:
    Value:
      Ref: ExternalId
`))

// Template renders the CloudFormation template (YAML). Its values are validated first, so nothing a
// caller sends can change the template's structure.
func Template(p TemplateParams) (string, error) {
	if p.Access == "" {
		p.Access = AccessHub
	}
	if !ValidAccess(p.Access) {
		return "", fmt.Errorf("access must be %q or %q", AccessHub, AccessReadOnly)
	}
	if p.HubPrincipal != "" && !ValidPrincipalARN(p.HubPrincipal) {
		return "", fmt.Errorf("the Hub principal must be an IAM role or user ARN")
	}
	if p.ExternalID != "" && RoleName(p.ExternalID) == "" {
		return "", fmt.Errorf("not an External ID made by the Hub")
	}
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, map[string]any{
		"HubPrincipal": p.HubPrincipal, "ExternalID": p.ExternalID, "RoleName": RoleName(p.ExternalID),
		"Access": p.Access, "Actions": HubActions, "ReadOnlyPolicy": ReadOnlyPolicy, "RolePrefix": RolePrefix,
	})
	return buf.String(), err
}

// Parameters are the template parameter values for one connection.
func Parameters(hubPrincipal, externalID, access string) [][2]string {
	return [][2]string{{"HubPrincipalArn", hubPrincipal}, {"ExternalId", externalID}, {"RoleName", RoleName(externalID)}, {"AccessLevel", access}}
}

// QuickCreateURL is the AWS console's quick-create link: it opens "Quick create stack" with the
// template and every parameter filled in, so the user only ticks the IAM acknowledgement and clicks
// Create stack. templateURL must be an https URL of an object in Amazon S3 (the console accepts nothing
// else); "" when the partition has no standard console or no template URL is set.
func QuickCreateURL(templateURL, region, hubPrincipal, externalID, access string) string {
	if templateURL == "" || Partition(hubPrincipal) != "aws" {
		return ""
	}
	if region == "" {
		region = "us-east-1"
	}
	q := []string{"templateURL=" + url.QueryEscape(templateURL), "stackName=" + url.QueryEscape(StackName(externalID))}
	for _, kv := range Parameters(hubPrincipal, externalID, access) {
		q = append(q, "param_"+kv[0]+"="+url.QueryEscape(kv[1]))
	}
	return "https://" + region + ".console.aws.amazon.com/cloudformation/home?region=" + url.QueryEscape(region) +
		"#/stacks/create/review?" + strings.Join(q, "&")
}

// TemplateFile is the file name the template downloads as.
func TemplateFile(externalID string) string {
	if s := StackName(externalID); s != "" {
		return s + ".yaml"
	}
	return "doctherepo-hub-readonly.yaml"
}

// DeployCommand is the AWS CLI command that creates the stack from the downloaded template, for when no
// quick-create link is possible.
func DeployCommand(region, hubPrincipal, externalID, access string) string {
	parts := []string{"aws cloudformation deploy", "--stack-name " + StackName(externalID), "--template-file " + TemplateFile(externalID),
		"--capabilities CAPABILITY_NAMED_IAM"}
	if region != "" {
		parts = append(parts, "--region "+region)
	}
	po := "--parameter-overrides"
	for _, kv := range Parameters(hubPrincipal, externalID, access) {
		po += " " + kv[0] + "=" + kv[1]
	}
	return strings.Join(append(parts, po), " ")
}
