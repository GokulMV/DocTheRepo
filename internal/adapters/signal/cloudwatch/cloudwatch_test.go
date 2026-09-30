package cloudwatch

import (
	"context"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type fakeLogs struct {
	events []logtypes.FilteredLogEvent
	calls  []*cloudwatchlogs.FilterLogEventsInput
}

// FilterLogEvents returns matching events two per page.
func (f *fakeLogs) FilterLogEvents(_ context.Context, in *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	cp := *in
	f.calls = append(f.calls, &cp)
	var match []logtypes.FilteredLogEvent
	if *in.LogGroupName != "/ecs/payments" {
		return &cloudwatchlogs.FilterLogEventsOutput{}, nil
	}
	for _, e := range f.events {
		if ts := *e.Timestamp; ts >= *in.StartTime && ts <= *in.EndTime {
			match = append(match, e)
		}
	}
	off := 0
	if in.NextToken != nil {
		off = int((*in.NextToken)[0] - '0')
	}
	end := min(off+2, len(match))
	out := &cloudwatchlogs.FilterLogEventsOutput{Events: match[off:end]}
	if end < len(match) {
		out.NextToken = awssdk.String(string(rune('0' + end)))
	}
	return out, nil
}

type fakeAlarms struct{ items []cwtypes.AlarmHistoryItem }

func (f *fakeAlarms) DescribeAlarmHistory(_ context.Context, in *cw.DescribeAlarmHistoryInput, _ ...func(*cw.Options)) (*cw.DescribeAlarmHistoryOutput, error) {
	var out []cwtypes.AlarmHistoryItem
	for _, it := range f.items {
		if !it.Timestamp.Before(*in.StartDate) && !it.Timestamp.After(*in.EndDate) {
			out = append(out, it)
		}
	}
	return &cw.DescribeAlarmHistoryOutput{AlarmHistoryItems: out}, nil
}

func logEv(id string, ts int64, msg string) logtypes.FilteredLogEvent {
	return logtypes.FilteredLogEvent{EventId: awssdk.String(id), Timestamp: awssdk.Int64(ts), Message: awssdk.String(msg), LogStreamName: awssdk.String("s1")}
}

func TestPollLogsAndAlarms(t *testing.T) {
	now := time.UnixMilli(1790000600000).UTC()
	logs := &fakeLogs{events: []logtypes.FilteredLogEvent{
		logEv("e1", 1790000500000, "ERROR payment failed for order 1"),
		logEv("e2", 1790000510000, "INFO retrying"),
		logEv("e3", 1790000520000, "ERROR payment failed for order 2"),
		logEv("e4", 1790000520000, "Exception in thread \"main\" java.lang.IllegalStateException: closed\n\tat com.acme.Pool.get(Pool.java:10)"),
		logEv("e5", 1790000530000, "ERROR payment failed for order 3"),
	}}
	alarms := &fakeAlarms{items: []cwtypes.AlarmHistoryItem{
		{AlarmName: awssdk.String("api-5xx"), Timestamp: awssdk.Time(now.Add(-time.Minute)), HistorySummary: awssdk.String("Alarm updated from OK to ALARM"),
			HistoryData: awssdk.String(`{"newState":{"stateValue":"ALARM","stateReason":"Threshold Crossed: 42 > 10"}}`)},
		{AlarmName: awssdk.String("api-5xx"), Timestamp: awssdk.Time(now.Add(-30 * time.Second)),
			HistoryData: awssdk.String(`{"newState":{"stateValue":"OK"}}`)},
	}}
	p := &Poller{Now: func() time.Time { return now }, Connect: func(context.Context, ports.ConnectorConfig) (Clients, error) {
		return Clients{Logs: logs, Alarms: alarms, Account: "123456789012", Region: "eu-west-1"}, nil
	}}
	cc := ports.ConnectorConfig{ID: "c1", Type: "cloudwatch", Config: map[string]string{"log_groups": "/ecs/payments", "region": "eu-west-1"}}
	cursors := map[string]string{}
	var got []ports.SignalEvent
	emit := func(stream, cursor string, evs []ports.SignalEvent) error {
		cursors[stream] = cursor
		got = append(got, evs...)
		return nil
	}
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	require.Len(t, got, 5, "four log lines (INFO dropped) and one alarm")
	assert.Equal(t, *logs.calls[0].StartTime, now.Add(-5*time.Minute).UnixMilli(), "first poll reads the lookback")
	assert.Equal(t, DefaultFilterPattern, *logs.calls[0].FilterPattern)
	assert.Len(t, logs.calls, 3, "all pages read")
	assert.Equal(t, "java.lang.IllegalStateException", got[2].ExceptionType)
	assert.Equal(t, "payments", got[0].Attrs["service.derived"])
	alarm := got[4]
	assert.Equal(t, ports.KindAlert, alarm.Kind)
	assert.Equal(t, "arn:aws:cloudwatch:eu-west-1:123456789012:alarm:api-5xx", alarm.RuleID, "same rule as the EventBridge path")
	assert.Equal(t, "Threshold Crossed: 42 > 10", alarm.Message)

	// The next poll starts at the newest timestamp and skips what it already emitted there.
	logs.events = append(logs.events, logEv("e6", 1790000530000, "ERROR payment failed for order 4"))
	got = nil
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	require.Len(t, got, 1)
	assert.Equal(t, "e6", got[0].ExternalID)
	assert.Equal(t, int64(1790000530000), *logs.calls[len(logs.calls)-1].StartTime)

	// A quiet group moves its cursor to the window's end.
	quiet := ports.ConnectorConfig{ID: "c2", Config: map[string]string{"log_groups": "/aws/lambda/idle", "alarms": "false"}}
	qc := map[string]string{}
	require.NoError(t, p.Poll(context.Background(), quiet, qc, func(s, c string, _ []ports.SignalEvent) error { qc[s] = c; return nil }))
	assert.Equal(t, "1790000600000|", qc["logs:/aws/lambda/idle"])
	assert.NotContains(t, qc, "alarms")
}

func TestConnectValidates(t *testing.T) {
	var ve *ports.ValidationError
	_, err := Connect(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	assert.ErrorAs(t, err, &ve)
	_, err = Connect(context.Background(), ports.ConnectorConfig{Credentials: `{"access_key_id":""}`, Config: map[string]string{"region": "eu-west-1"}})
	assert.ErrorAs(t, err, &ve)
}
