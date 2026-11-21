BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Outbox relay (see docs/superpowers/specs/2026-10-05-outbox-relay-design.md).
--
-- attempts counts the times the broker permanently rejected an event; transient failures (the broker being down) are not
-- counted. parked_at is set when the relay gives up on an event after the maximum number of attempts; the row stays
-- visible with its error_message and can be re-queued with: UPDATE outbox_events SET parked_at = NULL, attempts = 0 WHERE id = '...'
--
-- The first index serves the relay's claim query (unsent, unparked, undeleted rows per destination in creation order),
-- the second its purge of old sent rows.
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
ALTER TABLE outbox_events
    ADD COLUMN attempts  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN parked_at TIMESTAMP WITH TIME ZONE NULL;

COMMENT ON COLUMN outbox_events.attempts IS 'Times the broker permanently rejected this event; transient failures are not counted';
COMMENT ON COLUMN outbox_events.parked_at IS 'When the relay gave up on this event after the maximum number of attempts; null if not parked';

CREATE INDEX ix_outbox_events_unsent_idx ON outbox_events (destination, created_at, id)
    WHERE sent_time IS NULL AND parked_at IS NULL AND deleted_at IS NULL;
CREATE INDEX ix_outbox_events_sent_time_purge_idx ON outbox_events (sent_time)
    WHERE sent_time IS NOT NULL;

COMMIT;
