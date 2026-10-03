<!-- dth:generated source="internal/store/repos.go" — edit only inside dth:human blocks -->
# `internal/store/repos.go`

<!-- dth:chunk 2f46e30d0338abed -->
## `defaultPollSeconds`

Determines the default polling interval in seconds for a connector based on its type. Event-platform inspectors (Kafka, SQS, SNS, EventBridge, Kinesis, Pub/Sub, RabbitMQ) poll every 30 seconds; knowledge sources (Confluence, Jira, Notion) every 15 minutes (900 seconds); Wiz every 5 minutes (300 seconds) due to API rate limits and slow-changing security data; all other connector types default to 60 seconds. Returns `int64` to match the storage representation of polling intervals.
