# Wiz and Splunk

Both are signal sources: what they send ends up in the Inbox, grouped, matched against known issues, and
(unless you turn it off) explained.

## Keep a source away from models

Every signal source has a **Never send this source's data to a model** option. It sets
`never_send_to_llm: true` in the connector config, and the Add dialog ticks it by default for Wiz. For
issues from such a source:

- **Decoding:** the issue is never decoded, not even by "Explain again". The issue page shows why.
- **Rule proposals:** "From text", "from a link", and daily suggestions never show the model these issues.
- **What still works:** grouping, known-issue rules, counts, and the Inbox, because none of them need a
  model.

## Wiz

Wiz issues become **security findings**, one per rule and resource. The fingerprint is
`sha256("security" + "wiz" + rule + resource)`. Repeat notifications for the same finding land on the same
issue, and a new resource opens a new one.

| Wiz severity | Hub severity |
|---|---|
| CRITICAL | critical |
| HIGH | error |
| MEDIUM | warning |
| LOW / INFORMATIONAL | info |

**Pushing from a Wiz webhook integration.** Point a webhook integration at the URL the Hub shows, and send
the connector secret as a bearer token, basic-auth password, or `X-DTH-Token` header.
- **What it reads:** the default issue template, `{trigger, issue, resource, control}`. The GraphQL issue
  shape (a template that forwards the issue object) also works.
- **What it ignores:** resolved and rejected issues.

**Polling the Wiz GraphQL API** (every 5 minutes).
- **Setup:** create a service account with `read:issues`, then set the following on the connector:
  - `api_url`: your tenant endpoint, e.g. `https://api.us17.app.wiz.io/graphql`.
  - Credentials: `{"client_id": "…", "client_secret": "…"}`.
- **What each poll reads:** `issuesV2` with status `OPEN`/`IN_PROGRESS`, updated since the cursor, 500 per
  page, at most 20 pages.
- **Optional settings:**
  - `statuses`, e.g. `OPEN,IN_PROGRESS`.
  - `severities`, e.g. `CRITICAL,HIGH`.
  - `auth_url`: defaults to `https://auth.app.wiz.io/oauth/token`. Gov tenants use their own.
  - `lookback_hours`: how far back the first poll reads, default 24.
  - `time_filter`: the filter field compared with the cursor, default `updatedAt`.

**Which service and environment a finding is attached to.**
- **Service:** the resource tag named in `service_tag` (default `service`), then the `app` and
  `application` tags.
- **Environment:** the `env` or `environment` tag, then the subscription name.

## Splunk

**Webhook alert action.** In the alert, add the **Webhook** action with the Hub's URL, and append
`?token=<connector secret>`, because Splunk's webhook action cannot send headers.
- **What one firing becomes:** one alert, built from Splunk's payload
  `{result, sid, results_link, search_name, app, owner}`.
- **Grouping:** firings group by saved search and service.
- **Skipped firings:** a firing with an empty result row (an alert set to trigger even without results).

**Polling.** The poller runs searches through the REST API (the management port, usually `8089`):
- **Each poll:** each listed saved search (`saved_searches`) or SPL query (`queries`, one per line) runs as
  a oneshot job over the window since the last poll.
- **Window:** it ends `lag_seconds` (default 60) in the past, to allow for indexing delay.
- **Result rows:** each row becomes a log event. Multi-line events keep their stack traces, so a Java or
  Python exception groups like it does from any other log source. Rows below `min_severity` (default
  `warning`) are dropped. Rows without a message, such as `stats` output, become `key=value` text.
- **Credentials:** an authentication token, or `{"username","password"}`. The account needs the `search`
  capability, and no other.
- **Other settings:**
  - `app`: the namespace, default `search`.
  - `lookback_minutes`: how far back the first poll reads, default 15.
  - `max_results`: rows per search per poll, default 5000.
  - `field.service` / `field.environment` / `field.severity` / `field.message`: which result fields to
    read for those values.

| Splunk severity | Hub severity |
|---|---|
| 5 severe, 6 fatal, critical | critical |
| 4 error, high | error |
| 3 warn, medium | warning |
| 1 debug, 2 info, low | info |

Splunk Enterprise Security notables arrive the same way: send them through a webhook alert action, or
poll a notable search.

## Verify on first use

Payload shapes come from the vendors' documented formats. This build environment could not reach Wiz or
Splunk, so the adapters are tested against fixtures and fake servers. Check the following before relying
on them:

- **Wiz, polling:** after the first poll, the connector's health is `ok`. A GraphQL error is shown verbatim
  in the connector's last error. If your tenant rejects the `updatedAt` filter, set `time_filter` to
  `statusChangedAt`.
- **Wiz, webhook:** send a test notification from the integration. It should show up in the Inbox under
  source **Wiz**, kind **security findings**.
- **Splunk:** run one saved search by hand with the same window. Its row count should match the events the
  Hub received, which are listed per search in the job result (`GET /api/v1/jobs?type=signal_batch`).
