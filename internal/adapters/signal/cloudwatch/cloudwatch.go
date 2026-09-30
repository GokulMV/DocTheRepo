// Package cloudwatch polls CloudWatch for installs without a subscription or EventBridge rule (plan
// § 8.16): Logs FilterLogEvents per configured log group, and alarm history for transitions into ALARM.
// Cross-account access uses STS AssumeRole with the connector's external ID; the role needs only
// logs:FilterLogEvents and cloudwatch:DescribeAlarmHistory.
package cloudwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/aws"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultFilterPattern matches problems the way the subscription-filter setup does.
const DefaultFilterPattern = "?ERROR ?Exception ?Traceback ?FATAL ?panic"

// Limits per poll and stream: a log group noisier than this belongs on a subscription filter.
const (
	maxPages = 20
	window   = 15 * time.Minute
)

// LogsAPI and AlarmsAPI are the read calls used (the SDK clients, or fakes).
type LogsAPI interface {
	FilterLogEvents(ctx context.Context, in *cloudwatchlogs.FilterLogEventsInput, opts ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

// AlarmsAPI reads alarm history.
type AlarmsAPI interface {
	DescribeAlarmHistory(ctx context.Context, in *cw.DescribeAlarmHistoryInput, opts ...func(*cw.Options)) (*cw.DescribeAlarmHistoryOutput, error)
}

// Clients are one connector's API clients.
type Clients struct {
	Logs    LogsAPI
	Alarms  AlarmsAPI
	Account string
	Region  string
}

// Poller implements ports.SignalPoller.
type Poller struct {
	// Connect builds clients (default: the AWS SDK); results are cached per connector settings.
	Connect func(ctx context.Context, cc ports.ConnectorConfig) (Clients, error)
	Now     func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	key string
	c   Clients
}

// New returns a poller using the AWS SDK.
func New() *Poller { return &Poller{Connect: Connect} }

// Type implements ports.SignalPoller.
func (*Poller) Type() string { return "cloudwatch" }

// Poll implements ports.SignalPoller. Streams: "logs:<group>" per log group, "alarms".
func (p *Poller) Poll(ctx context.Context, cc ports.ConnectorConfig, cursors map[string]string,
	emit func(stream, cursor string, events []ports.SignalEvent) error) error {
	c, err := p.clients(ctx, cc)
	if err != nil {
		return err
	}
	now := p.now()
	lookback := lookback(cc.Config)
	for _, g := range groups(cc.Config) {
		if err := p.pollGroup(ctx, c, cc, g, cursors["logs:"+g], now, lookback, emit); err != nil {
			return fmt.Errorf("log group %s: %w", g, err)
		}
	}
	if cc.Config["alarms"] != "false" {
		if err := p.pollAlarms(ctx, c, cc, cursors["alarms"], now, lookback, emit); err != nil {
			return fmt.Errorf("alarm history: %w", err)
		}
	}
	return nil
}

func (p *Poller) pollGroup(ctx context.Context, c Clients, cc ports.ConnectorConfig, group, raw string, now time.Time,
	lookback time.Duration, emit func(string, string, []ports.SignalEvent) error) error {
	cur := sigutil.ParseCursor(raw)
	start := now.Add(-lookback).UnixMilli()
	if ms, err := strconv.ParseInt(cur.At, 10, 64); err == nil {
		start = ms
	}
	end := min(now.UnixMilli(), time.UnixMilli(start).Add(window).UnixMilli())
	pattern := cc.Config["filter_pattern"]
	if pattern == "" {
		pattern = DefaultFilterPattern
	}
	minSev := loglines.MinSeverity(cc.Config)
	in := &cloudwatchlogs.FilterLogEventsInput{LogGroupName: awssdk.String(group), StartTime: awssdk.Int64(start),
		EndTime: awssdk.Int64(end), FilterPattern: awssdk.String(pattern)}
	next := cur
	var events []ports.SignalEvent
	for page := 0; page < maxPages; page++ {
		out, err := c.Logs.FilterLogEvents(ctx, in)
		if err != nil {
			return err
		}
		for _, e := range out.Events {
			id, ts := awssdk.ToString(e.EventId), awssdk.ToInt64(e.Timestamp)
			at := strconv.FormatInt(ts, 10)
			if cur.Skip(at, id) {
				continue
			}
			next.Advance(at, id, lessInt)
			ev := aws.LogEvent(cc.ID, group, awssdk.ToString(e.LogStreamName), c.Account, id, ts, awssdk.ToString(e.Message))
			if loglines.Keep(ev, minSev) {
				events = append(events, ev)
			}
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	// Results interleave log streams, so the cursor moves only once the window is read. When nothing
	// matched, the window's end is the new start (no events can appear before it later).
	if next.At == cur.At && len(events) == 0 && end > start {
		next = sigutil.Cursor{At: strconv.FormatInt(end, 10), Seen: map[string]bool{}}
	}
	return emit("logs:"+group, next.String(), events)
}

// historyData is DescribeAlarmHistory's StateUpdate payload.
type historyData struct {
	NewState struct {
		StateValue  string `json:"stateValue"`
		StateReason string `json:"stateReason"`
	} `json:"newState"`
}

func (p *Poller) pollAlarms(ctx context.Context, c Clients, cc ports.ConnectorConfig, raw string, now time.Time,
	lookback time.Duration, emit func(string, string, []ports.SignalEvent) error) error {
	cur := sigutil.ParseCursor(raw)
	start := now.Add(-lookback)
	if t, err := time.Parse(time.RFC3339Nano, cur.At); err == nil {
		start = t
	}
	in := &cw.DescribeAlarmHistoryInput{HistoryItemType: cwtypes.HistoryItemTypeStateUpdate, StartDate: awssdk.Time(start),
		EndDate: awssdk.Time(now), ScanBy: cwtypes.ScanByTimestampAscending, MaxRecords: awssdk.Int32(100)}
	next := cur
	var events []ports.SignalEvent
	for page := 0; page < maxPages; page++ {
		out, err := c.Alarms.DescribeAlarmHistory(ctx, in)
		if err != nil {
			return err
		}
		for _, h := range out.AlarmHistoryItems {
			name, ts := awssdk.ToString(h.AlarmName), awssdk.ToTime(h.Timestamp).UTC()
			at, id := ts.Format(time.RFC3339Nano), name
			if cur.Skip(at, id) {
				continue
			}
			next.Advance(at, id, lessTime)
			var d historyData
			_ = json.Unmarshal([]byte(awssdk.ToString(h.HistoryData)), &d)
			if d.NewState.StateValue != "ALARM" {
				continue // recoveries and INSUFFICIENT_DATA are not problems
			}
			ruleID := name
			if c.Account != "" && c.Region != "" { // the ARN, as EventBridge reports it, so both paths group together
				ruleID = "arn:aws:cloudwatch:" + c.Region + ":" + c.Account + ":alarm:" + name
			}
			events = append(events, ports.SignalEvent{ConnectorID: cc.ID, Source: "cloudwatch", Kind: ports.KindAlert,
				Severity: ports.SeverityError, ExternalID: name + "@" + at, OccurredAt: ts, RuleID: ruleID, Title: name,
				Message: firstNonEmpty(d.NewState.StateReason, awssdk.ToString(h.HistorySummary)),
				Attrs:   map[string]string{"alarm_name": name, "aws.account": c.Account, "aws.region": c.Region}})
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	if next.At == "" {
		next.At = start.UTC().Format(time.RFC3339Nano)
	}
	return emit("alarms", next.String(), events)
}

func (p *Poller) clients(ctx context.Context, cc ports.ConnectorConfig) (Clients, error) {
	key := cc.Credentials + "\x00" + cc.Config["region"] + "\x00" + cc.Config["role_arn"] + "\x00" + cc.Config["external_id"]
	p.mu.Lock()
	if c, ok := p.cache[cc.ID]; ok && c.key == key {
		p.mu.Unlock()
		return c.c, nil
	}
	p.mu.Unlock()
	c, err := p.Connect(ctx, cc)
	if err != nil {
		return c, err
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = map[string]cached{}
	}
	p.cache[cc.ID] = cached{key: key, c: c}
	p.mu.Unlock()
	return c, nil
}

// staticKey is an optional access key in the connector's credentials (otherwise the default chain: the
// Hub's task role, instance profile, or environment).
type staticKey struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

// Connect builds SDK clients from LoadConfig.
func Connect(ctx context.Context, cc ports.ConnectorConfig) (Clients, error) {
	cfg, account, err := LoadConfig(ctx, cc)
	if err != nil {
		return Clients{}, err
	}
	return Clients{Logs: cloudwatchlogs.NewFromConfig(cfg), Alarms: cw.NewFromConfig(cfg), Account: account, Region: cfg.Region}, nil
}

// LoadConfig builds an AWS config for a connector: region from config, credentials from the connector or
// the default chain, then AssumeRole into role_arn with external_id when set. It verifies the credentials
// (STS GetCallerIdentity) and returns the account ID. Every AWS signal adapter uses it.
func LoadConfig(ctx context.Context, cc ports.ConnectorConfig) (awssdk.Config, string, error) {
	region := strings.TrimSpace(cc.Config["region"])
	if region == "" {
		return awssdk.Config{}, "", &ports.ValidationError{Code: "INVALID_CONFIG", Message: "region is required"}
	}
	opts := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if strings.TrimSpace(cc.Credentials) != "" {
		var k staticKey
		if err := json.Unmarshal([]byte(cc.Credentials), &k); err != nil || k.AccessKeyID == "" || k.SecretAccessKey == "" {
			return awssdk.Config{}, "", &ports.ValidationError{Code: "INVALID_CREDENTIALS", Message: `credentials must be {"access_key_id","secret_access_key"}`}
		}
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(k.AccessKeyID, k.SecretAccessKey, k.SessionToken)))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awssdk.Config{}, "", fmt.Errorf("aws config: %w", err)
	}
	if role := strings.TrimSpace(cc.Config["role_arn"]); role != "" {
		ext := strings.TrimSpace(cc.Config["external_id"])
		cfg.Credentials = awssdk.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), role, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "doctherepo-hub"
			if ext != "" {
				o.ExternalID = awssdk.String(ext)
			}
		}))
	}
	id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return awssdk.Config{}, "", fmt.Errorf("aws credentials: %w", err)
	}
	return cfg, awssdk.ToString(id.Account), nil
}

func groups(cfg map[string]string) []string {
	var out []string
	for _, g := range strings.Split(cfg["log_groups"], ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// lookback is how far the first poll reads back (config lookback_minutes, default 5, at most 1440).
func lookback(cfg map[string]string) time.Duration {
	n, err := strconv.Atoi(cfg["lookback_minutes"])
	if err != nil || n <= 0 {
		n = 5
	}
	return time.Duration(min(n, 1440)) * time.Minute
}

func lessInt(a, b string) bool {
	x, _ := strconv.ParseInt(a, 10, 64)
	y, _ := strconv.ParseInt(b, 10, 64)
	return x < y
}

func lessTime(a, b string) bool {
	x, _ := time.Parse(time.RFC3339Nano, a)
	y, _ := time.Parse(time.RFC3339Nano, b)
	return x.Before(y)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (p *Poller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
