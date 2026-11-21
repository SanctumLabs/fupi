-- name: QueryCreateKafkaOutboxEvent :one
INSERT INTO 
  kafka_outbox_events (
    partition_key,
    correlation_id,
    destination,
    event_type,
    payload,
    error_message,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: QueryUpdateKafkaOutboxEvent :one
UPDATE kafka_outbox_events AS koe
SET
  error_message=$2,
  sent_time=$3,
  destination=$4,
  updated_at=now()
WHERE koe.id = $1 RETURNING *;

-- name: QueryUpdateKafkaOutboxEventError :one
UPDATE kafka_outbox_events AS koe
SET
  error_message=$2,
  updated_at=now()
WHERE koe.id = $1 RETURNING *;

-- name: QuerySoftDeleteKafkaOutboxEvent :one
UPDATE kafka_outbox_events
SET
  deleted_at = $2,
  updated_at = now()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteKafkaOutboxEvent :one
DELETE FROM kafka_outbox_events WHERE id = $1 RETURNING *;

-- name: QueryMarkKafkaOutboxEventAsSent :one
UPDATE kafka_outbox_events
SET
  sent_time=$2,
  updated_at=now()
WHERE id = $1 RETURNING *;
