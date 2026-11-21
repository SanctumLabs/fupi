-- name: QueryCreateOutboxEvent :one
INSERT INTO 
  outbox_events (
    group_id,
    correlation_id,
    destination,
    event_type,
    headers,
    payload,
    error_message,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: QueryUpdateOutboxEvent :one
UPDATE outbox_events AS oe
SET
  error_message=$2,
  sent_time=$3,
  destination=$4,
  updated_at=now()
WHERE oe.id = $1 RETURNING *;

-- name: QueryUpdateOutboxEventError :one
UPDATE outbox_events AS oe
SET
  error_message=$2,
  updated_at=now()
WHERE oe.id = $1 RETURNING *;

-- name: QuerySoftDeleteOutboxEvent :one
UPDATE outbox_events
SET
  deleted_at = $2,
  updated_at=now()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteOutboxEvent :one
DELETE FROM outbox_events WHERE id = $1 RETURNING *;

-- name: QueryMarkOutboxEventAsSent :one
UPDATE outbox_events
SET
  sent_time=$2,
  updated_at=now()
WHERE id = $1 RETURNING *;

-- name: QueryMarkOutboxEventAsPermanentlyFailed :one
UPDATE outbox_events
SET
  metadata = COALESCE(
    NULLIF(metadata::text, 'null')::jsonb,
    '{}'::jsonb
  ) || jsonb_build_object('failure_reason', 'PERMANENTLY_FAILED'),
  updated_at = now(),
  deleted_at = now()
WHERE id = $1 
RETURNING *;

-- name: QueryUpdateOutboxEventAsProcessing :one
UPDATE outbox_events
SET
  processing_at=$2,
  updated_at=now()
WHERE id = $1 RETURNING *;
