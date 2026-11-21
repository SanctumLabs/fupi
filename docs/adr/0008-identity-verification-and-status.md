---
status: accepted
---

# Registration issues a crypto-random token; only Verify activates a user

A registered user is persisted `INACTIVE` with a verification token and becomes `ACTIVE` only through `User.Verify`, which checks the token matches and has not expired (15 minutes, as before v2) and records `UserVerified`. As with the URL aggregate in ADR-0002, no setter can move a user between statuses.

The token is 32 crypto-random bytes, hex-encoded. It is deliberately **not** derived from the user id: ids are UUIDv7 (ADR-0001) and encode a timestamp, which would make tokens partially predictable.

Persisting the transition needs both the `verified` flag and `status` to change together, so `UserWriteDatastore.MarkVerified` applies both in one transaction. Two separate calls could leave a user verified but still inactive.

## Fixing the pre-v2 verification flow

The old flow emailed `encoding.Encode(token)` but looked the user up by the raw token, so no verification link could ever match. v2 stores and emails the same value; an e2e test pins this.

## Consequences

- An unknown token and an expired one both answer `400`, never `404`: distinguishing them tells an attacker which tokens exist.
- Verification links are single-use — a second attempt fails with `ErrUserAlreadyVerified`.
