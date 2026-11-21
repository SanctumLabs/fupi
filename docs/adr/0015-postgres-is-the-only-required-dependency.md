---
status: accepted
---

# Postgres is the only dependency the API requires; Redis is optional and Kafka is not used

`GET /health/ready` answers 503 when Postgres is down or the process is draining, and 200 otherwise. When Redis is down it answers 200 with `"status":"degraded"` and `"redis":"down"`. The API starts without Redis and carries on; go-redis reconnects by itself when Redis returns.

Redis will be a cache, not the source of truth, so losing it slows redirects down but loses nothing. Postgres holds the data and the outbox, so without it the API cannot do its job.

The API process does not connect to Kafka at all. With the single outbox (ADR-0011) it writes `outbox_events` rows in the same Postgres transaction as the domain change, and only the relay worker publishes them. A Kafka outage therefore cannot affect the API; events wait in the outbox. The Kafka client is built with the relay.

## Consequences

- Liveness (`GET /health`) is always 200 while the process runs; it checks nothing, so an orchestrator does not restart the API because a dependency is down.
- The readiness body carries `up`/`down` per dependency and never error text, because the endpoint is public. Failures are logged with their error.
- A mis-set `REDIS_ADDRESS` shows as `degraded`, not as a crash. The startup log line (`redis is up` / `redis is down, continuing without it`) and the readiness body are the signals.
- When a feature starts depending on Redis for correctness (not just speed), that check must be registered as required.
