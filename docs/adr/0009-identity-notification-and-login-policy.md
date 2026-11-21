---
status: accepted
---

# Verification email is sent directly, and registration survives its failure

The context map has Identity publishing `UserRegistered` for the Notification context to consume, but no transactional outbox relay or Kafka adapter exists yet. Until one does, `Register` calls a `ports.Notifier` directly **and** the aggregate records `UserRegistered`, which the datastore commits to `outbox_events` with the user (ADR-0011). When the relay lands, the direct call is deleted and the queued event drives delivery; nothing else changes.

A failure to send the email does not fail the registration. The user row is already committed, so returning an error would tell the caller registration did not happen when it did. The failure is logged and the response is still `201`. This also matches the eventual outbox behaviour, where delivery is retried out of band.

No email transport is configured: `LoggingEmailSender` writes the message to the log, carrying over the pre-v2 email service, which was a no-op stub. It must be replaced before production.

## Login policy

- An unverified (`INACTIVE`) user **may** sign in, preserving pre-v2 behaviour.
- `SUSPENDED` and `DELETED` accounts may not, and get `403`.
- An unknown email and a wrong password are both `401` with the same message, so the endpoint cannot be used to enumerate accounts.
