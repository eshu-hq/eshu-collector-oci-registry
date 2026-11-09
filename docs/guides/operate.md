# Operate

What you'll actually see once claims are running, and what every
ending means. Run states (`complete`, `partial`, `retryable`,
`terminal`) are explained in [concepts](../concepts/README.md).

## What you'll see from the host

Today the host passes one thing from each result to the work item: the
failure class, such as `registry_rate_limited`, `invalid_config`, or
`claim_deadline_exceeded`. The collector fills in source wait time,
fact counts, and `retry_after_seconds` (the wait it asks for before a
retry), but the worker doesn't record or honour them yet, so no
dashboard shows them. It also writes one JSON line per claim to stderr
with the outcome, scope, failure class, step, HTTP status, and fact
count, and no addresses, bodies, or secrets. The host keeps only the
byte count and drops the text, so that line helps only in a local run
(`collector --sdk-stdio < request.json`) or in tests. This collector
emits no metrics or spans of its own; the manifest declares
`eshu_dp_oci_registry_collector_`, but nothing emits under it yet.

## Failure classes

Terminal means stop until someone fixes something. Retryable means a
later attempt may succeed. The classes below come from `failure.go`
and `internal/regfail/failure.go`.

Terminal:

| Class | What happened |
| --- | --- |
| `invalid_config` | The activation config can't run |
| `registry_auth_denied` | The registry refused the login (HTTP 401 or 403) |
| `registry_not_found` | The repository doesn't exist there (HTTP 404) |
| `registry_terminal_failure` | Any other non-retryable registry failure |
| `registry_canceled` | The claim's context was cancelled |
| `record_budget_exceeded` | Past the fact-count bound |
| `payload_budget_exceeded` | One fact past its payload-size bound |
| `result_budget_exceeded` | The encoded result past its size bound |

Retryable:

| Class | What happened |
| --- | --- |
| `claim_deadline_exceeded` | The claim deadline passed; no facts are returned |
| `registry_rate_limited` | The registry throttled the read (HTTP 429) |
| `registry_retryable_failure` | A transient transport error or a retryable status, including 408 and 5xx |

There's also `partial` to watch for, which isn't a failure at all:
the registry hid the Referrers API or the tag list outgrew the bounded
window, and both show up as `oci_registry.warning` facts.
