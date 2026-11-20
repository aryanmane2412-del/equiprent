# Monitoring

Not set up yet. The plan is in [docs/ARCHITECTURE.md §3](../../docs/ARCHITECTURE.md#3-technology-stack) (OpenTelemetry → Grafana stack) and [docs/SECURITY.md §8](../../docs/SECURITY.md#8-audit-and-monitoring).

## Signals available today

| Signal | Where |
|---|---|
| Liveness | `GET /healthz` (process up, never checks dependencies) |
| Readiness | `GET /readyz` (PostgreSQL ping, Redis ping or `disabled`; 503 when any enabled check fails. Error details hidden in production) |
| Request logs | One structured `slog` line per request: `request_id, method, route, status, latency_ms, bytes, client_ip`. JSON outside development |
| Panics | Logged with stack trace and request ID. Client gets a generic 500 problem response |
| Worker | Heartbeat log with dependency status and DB pool stats |

## Planned

- `/metrics` (Prometheus): request rate/latency/error by route, DB pool, job queue depth
- OpenTelemetry tracing (API → DB → jobs)
- Dashboards + alerts: 5xx rate, p95 latency, readiness failures, OTP request spikes, webhook signature failures
- Log shipping with 180-day retention in an India region
