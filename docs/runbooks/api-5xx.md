# Runbook: API 5xx error rate elevated

**Alert:** `ApiHigh5xxRate` (5xx ratio > 2% for 5m) — also fired indirectly by
`ApiHighP99Latency` when slowness tips requests into timeouts.

**Severity:** critical (availability error budget burn).

## Impact

A meaningful fraction of API requests are failing with 500/502/503/504.
Clients see errors; the availability SLO (99.5%) is burning down.

## First 5 minutes — triage

1. Open the **FuelGrid OS — API Overview** Grafana dashboard.
2. Check the **Request rate by status** and **Error rate** panels: is it all
   endpoints or one path? Use `sum by (path) (rate(fuelgrid_http_requests_total{status=~"5.."}[5m]))`.
3. Check the **Latency** panel — if p99 spiked alongside 5xx, the cause is
   likely downstream saturation (DB pool, slow query) rather than a code bug.
4. Check `up{job="fuelgrid-api"}` and `/readyz` — if readiness is failing, this
   is a dependency outage: jump to [db-unavailable.md](./db-unavailable.md).

## Common causes & actions

| Symptom                                             | Likely cause                       | Action                                                                 |
| --------------------------------------------------- | ---------------------------------- | ---------------------------------------------------------------------- |
| 5xx on all paths + readyz 503                       | Postgres/Redis down                | [db-unavailable.md](./db-unavailable.md)                               |
| 5xx + p99 latency spike + DB pool near max          | Pool saturation / slow query       | Identify slow query (`pg_stat_activity`); consider raising pool size   |
| 5xx isolated to one path right after a deploy       | Regression in new code             | Roll back the latest release; confirm error rate recovers              |
| 502/504 from the edge, app healthy                  | Ingress / proxy timeout            | Check ingress logs and upstream timeouts                               |
| Sudden 5xx with no deploy + memory climbing         | Resource exhaustion / leak         | Restart the affected instance(s); capture heap profile before restart |

## Investigation queries

```promql
# Error ratio by path
sum by (path) (rate(fuelgrid_http_requests_total{job="fuelgrid-api", status=~"5.."}[5m]))
  / sum by (path) (rate(fuelgrid_http_requests_total{job="fuelgrid-api"}[5m]))

# p99 latency by path
histogram_quantile(0.99, sum by (le, path)
  (rate(fuelgrid_http_request_duration_seconds_bucket{job="fuelgrid-api"}[5m])))
```

Pull structured logs/traces for the failing path (Sentry + traces are wired via
`internal/observability`). Correlate by trace ID.

## Mitigation

- **Bad deploy:** roll back to the previous known-good image.
- **Dependency:** follow the dependency-specific runbook; once the dependency
  recovers, readiness flips back to 200 and error rate should drop.
- **Overload:** scale out instances and/or shed load; verify pool size is sane.

## Recovery & follow-up

- Confirm the **Error rate** panel returns below 2% and the alert resolves.
- Record the incident, root cause, and error-budget impact.
- File follow-ups (regression test, capacity change, or a new alert) as needed.
