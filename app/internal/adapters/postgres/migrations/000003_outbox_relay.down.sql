BEGIN;

DROP INDEX IF EXISTS ix_outbox_events_sent_time_purge_idx;
DROP INDEX IF EXISTS ix_outbox_events_unsent_idx;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS parked_at,
    DROP COLUMN IF EXISTS attempts;

COMMIT;
