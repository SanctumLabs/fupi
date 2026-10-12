---
status: accepted
---

# The API publishes URLAccessed to Kafka best-effort, without the outbox

ADR-0015 kept Kafka out of the API and ADR-0011 sends every event through the transactional outbox. `URLAccessed` is the exception on both counts. A redirect has no database transaction to join, and writing a row per click to Postgres would put the database on the hottest path and load it with analytics. Losing some clicks is acceptable; slowing a redirect is not.

The API therefore builds a Kafka producer (through `ports.EventPublisher`) for this one use. `Record` queues the visit on a buffered channel and returns; one goroutine resolves the country through `ports.GeoLocator`, classifies the device from the user agent, and publishes to the `url.accessed` topic. When the buffer is full the visit is dropped and counted.

## Consequences

- Kafka stays optional for the API: the producer connects lazily, a broker outage only makes publishes fail (logged, dropped), and it is not a readiness check.
- No raw IP is published: it is used to resolve the country and then discarded, and `URLAccessed.IPAddress` is never set.
- Bots are published with `device_type: bot`, not dropped.
- At-most-once: a crash loses the queued visits. Consumers must not treat the stream as exact.
- Until a GeoIP database is chosen the country code is empty (`geolocation.Unknown`).
