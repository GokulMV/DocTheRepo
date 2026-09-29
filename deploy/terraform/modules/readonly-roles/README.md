# readonly-roles

Apply in every AWS account / GCP project the Hub should watch. Everything granted is read-only: the Hub
never writes to your cloud, never acks or deletes application messages, and never commits offsets for
application consumer groups.

- `aws/` — an IAM role the Hub's task role assumes (with an external ID) to read CloudWatch Logs/alarms/
  metrics and inspect SQS, SNS, EventBridge, Kinesis, and (optionally) MSK. SQS message peeking is opt-in
  because a receive increments `ApproximateReceiveCount`.
- `gcp/` — viewer roles for the Hub's service account (Logging, Error Reporting, Monitoring, Pub/Sub) and a
  Hub-owned subscription per dead-letter topic so DLQ messages can be sampled without touching application
  subscriptions.

Error-log shipping (CloudWatch subscription filter → Firehose → Hub, GCP log sink → Pub/Sub) arrives with
the signal ingest endpoints in Milestone 2 and will be added here then.
