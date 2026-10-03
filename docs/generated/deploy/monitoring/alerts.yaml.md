<!-- dth:generated source="deploy/monitoring/alerts.yaml" — edit only inside dth:human blocks -->
# `deploy/monitoring/alerts.yaml`

<!-- dth:chunk 18dd55dcbf1887d7 -->
## `deploy/monitoring/alerts.yaml`

Prometheus alerting rules for the DocTheRepo Hub service. This file defines nine alert rules that monitor key operational metrics: service availability (DTHHubDown), HTTP error rates (DTHHTTPErrors), job queue depth (DTHQueueBacklog), job failure rates (DTHJobsFailing), model provider call failures (DTHModelErrors), spend limit blocks (DTHSpendLimitBlocking), API latency percentiles (DTHAskSlow), signal ingestion backpressure (DTHSignalBackpressure), and event loss during overload (DTHSignalsDropped). Thresholds are tunable defaults; most warnings fire after 10–15 minutes of sustained conditions to reduce false alerts. Each rule includes severity labels (critical/warning/info) and annotations with summary and description for operator guidance.
