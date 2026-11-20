# EQUIPRENT Performance Report

## Status

No load test, profiler run, production trace, or benchmark was executed for this report. No throughput or latency target is claimed.

## Architecture observations

- PostgreSQL/PostGIS is the durable source of truth; spatial indexes and exclusion constraints are used for geo search and booking overlap protection.
- Redis can support cache, rate limits, pub/sub, and ephemeral state, but is optional in local configuration. Multi-instance production should use managed Redis and validate failover behavior.
- The Go API and worker have separate container targets. The API exposes health/readiness checks and uses graceful shutdown.
- Tracking WebSocket hub is process-local; live updates across replicas require a shared event bus.
- Flutter web and backend container build images are large; browser/CDN and mobile device performance were not measured.

## Required benchmark plan

1. Establish SLOs for p50/p95/p99 latency, error rate, availability, and cost per request.
2. Seed representative sanitized data and run k6 or equivalent against search, availability, booking creation, payment webhook, chat, tracking ingest, admin queries, and fleet reports.
3. Include booking contention tests proving no overlapping reservations under concurrent requests.
4. Profile PostgreSQL query plans and connection pools; verify spatial indexes, keyset pagination, partition maintenance, and slow-query alerts.
5. Measure WebSocket fanout/reconnect and worker backlog under bursts; test Redis loss and API replica scaling.
6. Measure Flutter startup, frame times, memory, web bundle size, image loading, and low-bandwidth behavior on representative devices.

## Commands

No benchmark harness is currently documented as configured. Add a versioned load-test suite before publishing performance claims. Local service startup is documented in `README.md` and `docker-compose.yml`; do not run load tests against production without an approved test window and bounded traffic profile.
