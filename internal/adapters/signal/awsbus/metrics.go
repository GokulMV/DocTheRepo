// Package awsbus inspects AWS event platforms read-only (plan § 8.20): SQS queue depth, oldest-message age,
// and dead-letter queues (discovered from RedrivePolicy); SNS delivery failures; EventBridge rule
// invocation failures; Kinesis and Lambda iterator age. Everything but the opt-in SQS peek is CloudWatch
// metrics or queue attributes.
package awsbus

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

// MetricsAPI is the CloudWatch read API used.
type MetricsAPI interface {
	ListMetrics(ctx context.Context, in *cw.ListMetricsInput, opts ...func(*cw.Options)) (*cw.ListMetricsOutput, error)
	GetMetricData(ctx context.Context, in *cw.GetMetricDataInput, opts ...func(*cw.Options)) (*cw.GetMetricDataOutput, error)
}

// metricQuery asks for one statistic of one metric.
type metricQuery struct {
	Namespace, Metric, Stat string
	Dims                    map[string]string
}

// metricWindow is how far back a query looks; metrics arrive every minute, a few minutes late.
const metricWindow = 10 * time.Minute

// latest returns each query's newest datapoint (absent when the metric had none in the window). Sum
// statistics are summed over the last 5 minutes instead, so a burst of failures is not missed between polls.
func latest(ctx context.Context, api MetricsAPI, qs []metricQuery, now time.Time) (map[int]float64, error) {
	out := map[int]float64{}
	for startIdx := 0; startIdx < len(qs); startIdx += 500 { // GetMetricData takes 500 queries
		batch := qs[startIdx:min(startIdx+500, len(qs))]
		in := &cw.GetMetricDataInput{StartTime: awssdk.Time(now.Add(-metricWindow)), EndTime: awssdk.Time(now),
			ScanBy: cwtypes.ScanByTimestampDescending}
		for i, q := range batch {
			var dims []cwtypes.Dimension
			for k, v := range q.Dims {
				dims = append(dims, cwtypes.Dimension{Name: awssdk.String(k), Value: awssdk.String(v)})
			}
			sort.Slice(dims, func(a, b int) bool { return *dims[a].Name < *dims[b].Name })
			in.MetricDataQueries = append(in.MetricDataQueries, cwtypes.MetricDataQuery{Id: awssdk.String(fmt.Sprintf("q%d", startIdx+i)),
				MetricStat: &cwtypes.MetricStat{Metric: &cwtypes.Metric{Namespace: awssdk.String(q.Namespace), MetricName: awssdk.String(q.Metric), Dimensions: dims},
					Period: awssdk.Int32(60), Stat: awssdk.String(q.Stat)}})
		}
		for {
			res, err := api.GetMetricData(ctx, in)
			if err != nil {
				return nil, err
			}
			for _, r := range res.MetricDataResults {
				var idx int
				if _, err := fmt.Sscanf(awssdk.ToString(r.Id), "q%d", &idx); err != nil || len(r.Values) == 0 {
					continue
				}
				if qs[idx].Stat == "Sum" {
					cut := now.Add(-5 * time.Minute)
					var sum float64
					for i, v := range r.Values {
						if i < len(r.Timestamps) && r.Timestamps[i].After(cut) {
							sum += v
						}
					}
					out[idx] += sum
					continue
				}
				if _, seen := out[idx]; !seen {
					out[idx] = r.Values[0] // descending: first is newest
				}
			}
			if res.NextToken == nil || *res.NextToken == "" {
				break
			}
			in.NextToken = res.NextToken
		}
	}
	return out, nil
}

// discover lists the dimension sets a metric is reported with (e.g. every SNS topic), up to 1,000.
func discover(ctx context.Context, api MetricsAPI, namespace, metric string) ([]map[string]string, error) {
	in := &cw.ListMetricsInput{Namespace: awssdk.String(namespace), MetricName: awssdk.String(metric),
		RecentlyActive: cwtypes.RecentlyActivePt3h}
	var out []map[string]string
	for page := 0; page < 20 && len(out) < 1000; page++ {
		res, err := api.ListMetrics(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, m := range res.Metrics {
			d := map[string]string{}
			for _, dim := range m.Dimensions {
				d[awssdk.ToString(dim.Name)] = awssdk.ToString(dim.Value)
			}
			out = append(out, d)
		}
		if res.NextToken == nil || *res.NextToken == "" {
			break
		}
		in.NextToken = res.NextToken
	}
	return out, nil
}

// allowed applies an optional comma-separated allow list (exact names or trailing-* prefixes).
func allowed(list, name string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		if p == name || (strings.HasSuffix(p, "*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*"))) {
			return true
		}
	}
	return false
}
