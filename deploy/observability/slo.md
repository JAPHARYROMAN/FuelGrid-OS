# FuelGrid OS — Service Level Objectives

These SLOs define the reliability targets for the FuelGrid API and the
alerting/dashboards in this directory. They are intentionally modest for an
early-stage single-region deployment and should be revisited as traffic and
the topology grow.

## Scope

- **Service:** `fuelgrid-api` (the Go HTTP API).
- **Measurement window:** 30-day rolling.
- **Data source:** Prometheus metrics emitted by
  `internal/observability/metrics.go`, scraped from `GET /metrics`.

## Objectives

| SLO          | Objective                                           | SLI (how we measure)                                                      |
| ------------ | --------------------------------------------------- | ------------------------------------------------------------------------- |
| Availability | 99.5% of requests succeed (non-5xx)                 | `1 - (sum(rate(5xx[window])) / sum(rate(all[window])))`                   |
| Latency      | 95% of requests < 300ms, 99% < 750ms                | `histogram_quantile` over `fuelgrid_http_request_duration_seconds_bucket` |
| Freshness    | Domain events published within 60s (p99 outbox lag) | `fuelgrid_outbox_oldest_unpublished_age_seconds`                          |

Health/readiness probe traffic (`/healthz`, `/readyz`, `/metrics`) is excluded
from the availability and latency SLIs where the dashboard/queries allow, since
it is not user-facing.

## Error budget

Availability target 99.5% over 30 days yields an error budget of **0.5%** of
requests, or roughly **3h 39m** of full unavailability per 30 days.

Budget policy:

- **> 50% budget remaining:** normal feature work proceeds.
- **< 50% budget remaining:** prioritise reliability fixes; require a reason to
  ship risky changes.
- **Budget exhausted:** freeze non-reliability deploys until the budget
  recovers; conduct a blameless post-incident review for the largest burner.

## Alerting alignment

Alerts in `prometheus-alerts.yml` are tuned to fire before the budget is fully
spent rather than as pure burn-rate alerts (kept simple for the current scale):

| Alert                     | Threshold                         | Relates to      |
| ------------------------- | --------------------------------- | --------------- |
| `ApiHigh5xxRate`          | 5xx ratio > 2% for 5m             | Availability    |
| `ApiHighP99Latency`       | p99 > 750ms for 10m               | Latency         |
| `ApiReadinessFailing`     | target down or /readyz 503 for 3m | Availability    |
| `DbPoolSaturation`        | acquired/max > 90% for 5m         | Latency (cause) |
| `OutboxDeadLetterBacklog` | unpublished > 500 for 10m         | Freshness       |
| `OutboxPublisherStalled`  | oldest unpublished > 300s for 5m  | Freshness       |

## Known gaps (must validate before relying on every panel/alert)

- **DB pool metrics are not yet emitted by the application.** The
  `DbPoolSaturation` alert and the DB pool dashboard panel reference
  `pgxpool_acquired_conns` / `pgxpool_max_conns`. These require registering a
  pgx pool stats collector (e.g. periodically reading `pool.Stat()` into gauges,
  or a community pgxpool Prometheus collector) in
  `internal/observability/metrics.go`. Until that exists, those signals are
  inert. All HTTP and outbox signals are backed by metrics that exist today.
- The `job="fuelgrid-api"` label depends on the scrape config naming; adjust
  the rules/dashboard variable if your Grafana Agent uses a different job name.
