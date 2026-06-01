# FuelGrid OS — Observability stack

This directory wires the FuelGrid API's already-exposed telemetry into a
runnable local monitoring stack: **Prometheus** scrapes `GET /metrics`,
**Grafana** renders dashboards, and **Alertmanager** routes the alerts defined
in `prometheus-alerts.yml`.

The API already emits everything; nothing here changes app code. This is the
glue that was missing: a scrape config, dashboards, provisioning, alert
routing, and a compose file to bring it all up against a locally-running API.

## Contents

| File                                     | Purpose                                                         |
| ---------------------------------------- | --------------------------------------------------------------- |
| `prometheus.yml`                         | Scrape config (targets the API `/metrics`) + alert-rule wiring. |
| `prometheus-alerts.yml`                  | Alerting rules (pre-existing; loaded by `prometheus.yml`).      |
| `alertmanager.yml`                       | Alert routing to a webhook / Slack placeholder.                 |
| `grafana/provisioning/datasources/`      | Auto-creates the Prometheus datasource (`uid: prometheus`).     |
| `grafana/provisioning/dashboards/`       | Dashboard provider that loads the JSON below.                   |
| `grafana/dashboards/http.json`           | HTTP: rate / latency / errors, overall and by route.            |
| `grafana/dashboards/business.json`       | Business: outbox backlog/lag/dead-letter, open shifts, journal. |
| `grafana/dashboards/scheduler.json`      | Scheduler: job success/failure rate and duration percentiles.   |
| `grafana-dashboard.json`                 | Legacy single-file dashboard for manual import (kept as-is).    |
| `slo.md`                                 | SLO definitions the alerts/dashboards are tuned to.             |
| `../../docker-compose.observability.yml` | Brings up Prometheus + Grafana + Alertmanager.                  |

## Quick start

The stack scrapes a FuelGrid API running **on the host** at `:8080` — not a
containerised API. Start the API first, then the stack.

```sh
# 1. Run the API on the host (defaults to :8080).
go run ./services/api/cmd/api
#   ...or the compiled binary; ensure /metrics is reachable:
curl -s localhost:8080/metrics | head

# 2. Bring up the observability stack (from the repo root).
docker compose -f docker-compose.observability.yml up -d

# 3. Open the UIs.
#    Prometheus     http://localhost:9090
#    Grafana        http://localhost:3001   (admin / admin)
#    Alertmanager   http://localhost:9093
```

Tear down: `docker compose -f docker-compose.observability.yml down`
(add `-v` to also drop the Prometheus/Grafana/Alertmanager data volumes).

### How the containers reach the host API

`prometheus.yml` and `alertmanager.yml` use `host.docker.internal` to reach the
host. Docker Desktop (macOS/Windows) provides this automatically; on Linux the
compose file adds `host.docker.internal:host-gateway` via `extra_hosts`.

If you containerise the API on the same compose network instead, change the
`fuelgrid-api` scrape target in `prometheus.yml` from `host.docker.internal:8080`
to your service name and port.

## Verifying it works

1. **Scrape is up:** Prometheus → Status → Targets. The `fuelgrid-api` target
   should be `UP`. If `DOWN`, the API isn't reachable at `host.docker.internal:8080`
   (is it running? is the port right?).
2. **Metrics flow:** Prometheus → Graph, run `fuelgrid_http_requests_total`.
   Generate traffic against the API and watch it climb.
3. **Dashboards:** Grafana → Dashboards → folder **FuelGrid OS** → three
   dashboards (HTTP, Business, Scheduler). The `$job` variable defaults to
   `fuelgrid-api`.
4. **Alerts loaded:** Prometheus → Status → Rules lists all groups from
   `prometheus-alerts.yml`. Alertmanager → it receives fired alerts.

## Alerts

Defined in `prometheus-alerts.yml`, tuned to the objectives in `slo.md`:

| Alert                     | Fires when                        | Severity | Routed via                 |
| ------------------------- | --------------------------------- | -------- | -------------------------- |
| `ApiHigh5xxRate`          | 5xx ratio > 2% for 5m             | critical | critical-webhook + default |
| `ApiReadinessFailing`     | target down or /readyz 503 for 3m | critical | critical-webhook + default |
| `ApiHighP99Latency`       | p99 > 750ms for 10m               | warning  | default-webhook            |
| `DbPoolSaturation`        | acquired/max conns > 90% for 5m   | warning  | default-webhook            |
| `OutboxDeadLetterBacklog` | unpublished > 500 for 10m         | warning  | default-webhook            |
| `OutboxPublisherStalled`  | oldest unpublished > 300s for 5m  | critical | critical-webhook + default |

Routing lives in `alertmanager.yml`: critical alerts use a tighter `group_wait`
and shorter `repeat_interval`. When `ApiReadinessFailing` fires, an inhibit rule
suppresses the derived API warnings so responders see one root-cause page.

### Wiring a real receiver

`alertmanager.yml` ships with **placeholder** webhook receivers pointing at
`host.docker.internal:5001`. To route to Slack, uncomment the `slack_configs`
block under `critical-webhook` and supply a real incoming-webhook URL (e.g. via
an `SLACK_WEBHOOK_URL` env substitution in your deployment), then restart
Alertmanager. To use a different sink, replace the `webhook_configs[].url`.

## Metric reference

Names emitted by `internal/observability/metrics.go` (all `fuelgrid_`-prefixed):

- HTTP: `fuelgrid_http_requests_total{method,path,status}`,
  `fuelgrid_http_request_duration_seconds_bucket{method,path,status}`,
  `fuelgrid_http_requests_inflight`.
- Outbox: `fuelgrid_outbox_unpublished_events`,
  `fuelgrid_outbox_oldest_unpublished_age_seconds`,
  `fuelgrid_outbox_dead_lettered_events`.
- Business: `fuelgrid_shifts_open`,
  `fuelgrid_accounting_journal_entries_posted`.
- Scheduler: `fuelgrid_scheduler_job_runs_total{job,result}`,
  `fuelgrid_scheduler_job_duration_seconds_bucket{job}`.

### `exported_job` on scheduler metrics

The scheduler counter/histogram carry their own `job` label (the _job name_).
Prometheus reserves `job` for the scrape job (`fuelgrid-api`), so on scrape the
metric's own label is renamed to **`exported_job`**. The scheduler dashboard
groups by `exported_job` accordingly. Don't confuse it with the `$job`
template variable, which is the scrape job (`fuelgrid-api`).

## Known gaps

- **DB pool metrics are not yet emitted by the app.** `DbPoolSaturation` and the
  related panel reference `pgxpool_acquired_conns` / `pgxpool_max_conns`, which
  require a pgx pool stats collector in `internal/observability/metrics.go`
  (see `slo.md`). Until then those signals are inert; everything else is backed
  by metrics that exist today.
- The `job="fuelgrid-api"` label is set by the scrape config here. If you rename
  the scrape job, update the alert rules and dashboard variables to match.
