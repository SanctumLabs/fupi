---
status: accepted
---

# Registration takes a username and a name, not just email and password

The pre-v2 `POST /auth/register` collected only an email and a password. The v2 schema cannot accept that: `users.username` is `NOT NULL UNIQUE` and `identity.NewUser` rejects an empty first name, so a legacy-shaped payload could never produce a valid row. Rather than weaken the schema or synthesise a username, registration now takes `username`, `first_name`, `last_name` (optional), `email` and `password`. This is a deliberate, breaking divergence from the pre-v2 API.

## Considered options

- **Derive the username from the email local-part** — silently invents identity data, and two people at different domains with the same local-part collide on a `UNIQUE` column with no good recovery.
- **Drop `NOT NULL UNIQUE` from `users.username`** — keeps the old payload, but the column then means nothing and nothing can look a user up by handle.

## Consequences

- Existing clients must send the new fields; the endpoint is not backward compatible.
- `last_name` is optional. `UserFullName.Value()` renders a first-name-only user without a trailing space.
