---
status: accepted
---

# Urls are scanned by the worker from `url.created`, a flagged url is suspended, and only an admin, checked against the datastore, can reinstate it

**Scan from the event, in the worker.** Nothing scans on create and the API never calls a scanner (ADR-0015). When `SAFE_BROWSING_API_KEY` is set, the worker (ADR-0018) joins the consumer group `fupi-url-scanner` on `url.events`, and for every `url.created` asks Google Safe Browsing (v4 `threatMatches:find`, the key in the `X-Goog-Api-Key` header so no error or log carries it) whether the target is malware or phishing. Without a key the worker logs that scanning is disabled and does not consume; it does not fail to start. A scanning worker also needs the Redis settings the API uses, which it reads only then.

**At least once, idempotent.** The consumer commits an offset only after the handler succeeded and handles records one at a time, so a crash or a failed commit redelivers. The handler skips a url that has a `url_scans` row from the provider, which makes redelivery harmless (the outbox already says consumers de-duplicate, ADR-0018); the scan is recorded last, so an event that failed after suspending the url is handled again, finds the url suspended and records the scan. An undecodable payload is logged and skipped, never retried.

**An outage is not a verdict.** A scanner that is down, slow, rate limiting or rejecting the key fails after a few bounded retries with `ErrScannerUnavailable`. Nothing is suspended and nothing is recorded; the handler returns the error, the offset stays uncommitted and the record is retried with a growing wait. The price is that the partition waits for the scanner: urls are not scanned while it is down, and a wrong key blocks scanning (and says so in the log on every retry) until it is fixed. A `safe` or `unknown` result suspends nothing and is recorded.

**Suspension.** A malware or phishing result calls `URL.Suspend(reason)` and `Update`, so `url.suspended` reaches the outbox in the same transaction, then evicts the short code and the custom alias from the redirect cache. Only an ACTIVE url can be suspended: for any other status, or a url that is gone, the scan is recorded and the event is done. A cache failure is logged and never fails the scan. A suspended link answers visitors with the same 404 as before; there is no interstitial.

**Roles.** `users.role` is a Postgres enum, `user` or `admin`, `user` by default. There is no API to change it; promote someone with `UPDATE users SET role = 'admin' WHERE email = '...'`. An admin who is demoted or suspended in SQL loses the privilege at once, because of how the check is made:

**The role is read from the datastore on every privileged call, not from the token.** The access token proves who the caller is and is valid for its whole lifetime (15 minutes by default); a role claim in it would keep a demoted admin an admin until it expires. `POST /api/v1/fupi/urls/:id/reinstate` loads the caller and requires `role = admin` and `status = ACTIVE`. One primary-key read on a rare, admin-only call costs nothing next to what staleness would. An unauthenticated caller gets 401, an authenticated non-admin 403, and a failed lookup 500, never a grant. Reinstating calls `URL.Reinstate()` and `Update` (`url.reinstated`) and evicts the cache.

## Considered options

- **The role in the JWT, verified against the datastore for reinstate** — the same read on the one call that needs it, plus a claim nobody trusts. Not carrying it is simpler.
- **Scanning inline on create** — puts a third party on the create path and turns its outage into ours. Ruled out by the issue.
- **Marking the url scanned as `unknown` when the scanner is down** — loses the event: the idempotency check would skip it afterwards. Retrying keeps it.
- **An interstitial warning page for suspended links** — a decision about product and about what we publish about a target; not made here. The link keeps returning 404.

## Consequences

- Suspension is eventually consistent: a url created at 10:00 is redirectable until the relay delivers its `url.created` and the scan returns, normally seconds, longer when the scanner is down.
- `Update` writes the status and expiry it loaded. Suspending or reinstating races any other writer of the same url (for example the expiry job) unless the update query guards on the status it read.
- The consumer group name and the topic are constants in the worker. A second environment sharing a Kafka cluster needs different ones.
- Migration `000005_user_role` adds the column; sqlc output, the schema file and the identity mapper carry the role.
