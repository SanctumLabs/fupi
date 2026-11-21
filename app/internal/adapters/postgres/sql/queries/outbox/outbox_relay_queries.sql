-- name: QueryClaimOutboxEvents :many
-- The relay's batch: per destination, the oldest unsent, unparked, undeleted events in creation order, so a destination
-- whose topic is unavailable cannot starve the others. The creation order is (created_at, id); id is a UUIDv7 and keeps
-- the events of one transaction in the order they were recorded.
--
-- Its cost depends on the batch size and the number of destinations, never on the size of the backlog: the recursive part
-- walks the distinct destinations with one index probe each (a skip scan over the partial index), and each destination
-- then reads only its first per_destination rows from the same index. A window function over the whole backlog would read
-- every waiting row to hand out one batch.
WITH RECURSIVE destinations AS (
  (
    SELECT oe.destination
    FROM outbox_events oe
    WHERE oe.sent_time IS NULL
      AND oe.parked_at IS NULL
      AND oe.deleted_at IS NULL
    ORDER BY oe.destination
    LIMIT 1
  )
  UNION ALL
  SELECT (
    SELECT oe.destination
    FROM outbox_events oe
    WHERE oe.destination > d.destination
      AND oe.sent_time IS NULL
      AND oe.parked_at IS NULL
      AND oe.deleted_at IS NULL
    ORDER BY oe.destination
    LIMIT 1
  )
  FROM destinations d
  WHERE d.destination IS NOT NULL
)
SELECT
  claimed.id,
  claimed.partition_key,
  claimed.destination,
  claimed.event_type,
  claimed.headers,
  claimed.payload,
  claimed.attempts,
  claimed.created_at
FROM destinations d
CROSS JOIN LATERAL (
  SELECT
    oe.id,
    oe.partition_key,
    oe.destination,
    oe.event_type,
    oe.headers,
    oe.payload,
    oe.attempts,
    oe.created_at
  FROM outbox_events oe
  WHERE oe.destination = d.destination
    AND oe.sent_time IS NULL
    AND oe.parked_at IS NULL
    AND oe.deleted_at IS NULL
  ORDER BY oe.created_at, oe.id
  LIMIT sqlc.arg(per_destination)::int
) claimed
WHERE d.destination IS NOT NULL
ORDER BY claimed.created_at, claimed.id;

-- name: QueryMarkOutboxEventsSent :execrows
UPDATE outbox_events
SET
  sent_time = now(),
  updated_at = now()
WHERE id = ANY(sqlc.arg(ids)::uuid[])
  AND sent_time IS NULL;

-- name: QueryRecordOutboxEventRejection :one
UPDATE outbox_events
SET
  attempts = attempts + 1,
  error_message = sqlc.arg(error_message),
  updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING attempts;

-- name: QueryParkOutboxEvent :exec
UPDATE outbox_events
SET
  parked_at = now(),
  error_message = sqlc.arg(error_message),
  updated_at = now()
WHERE id = sqlc.arg(id);

-- name: QueryPurgeSentOutboxEvents :execrows
DELETE FROM outbox_events
WHERE id IN (
  SELECT oe.id
  FROM outbox_events oe
  WHERE oe.sent_time IS NOT NULL
    AND oe.sent_time < sqlc.arg(older_than)::timestamptz
  ORDER BY oe.sent_time
  LIMIT sqlc.arg(limit_by)::int
);

-- name: QueryOutboxBacklog :one
SELECT
  count(*) FILTER (WHERE parked_at IS NULL)::bigint AS unsent,
  (min(created_at) FILTER (WHERE parked_at IS NULL))::timestamptz AS oldest_unsent,
  count(*) FILTER (WHERE parked_at IS NOT NULL)::bigint AS parked
FROM outbox_events
WHERE sent_time IS NULL
  AND deleted_at IS NULL;
