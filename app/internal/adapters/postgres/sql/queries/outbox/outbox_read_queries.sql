-- name: QueryAllOutboxEvents :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllOutboxEventsCount :one
SELECT
  count(*)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL;

-- name: QueryOutboxEventsByGroupId :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.group_id = sqlc.arg(group_id)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventByCorrelationId :one
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE oe.correlation_id = sqlc.arg(correlation_id);

-- name: QueryOutboxEventById :one
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE oe.id = $1;

-- name: QueryOutboxEventsByDestination :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.destination = $1
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventsByEventType :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.event_type = $1
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventsUnSent :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.sent_time IS NULL
-- Safely check for failure_reason
AND (oe.metadata IS NULL OR oe.metadata::text = 'null' OR (oe.metadata ->> 'failure_reason') IS NULL)
-- AND (
--   oe.metadata IS NULL 
--   OR oe.metadata::text = 'null'
--   OR (oe.metadata IS NOT NULL AND oe.metadata::text != 'null' AND NOT (oe.metadata ? 'failure_reason'))
-- )
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventsUnSentByDestinationWithLock :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND sqlc.arg(destination) = oe.destination
AND oe.sent_time IS NULL
-- Safely check for failure_reason
AND (oe.metadata IS NULL OR oe.metadata::text = 'null' OR (oe.metadata ->> 'failure_reason') IS NULL)
-- AND (
--   oe.metadata IS NULL 
--   OR oe.metadata::text = 'null'
--   OR (oe.metadata IS NOT NULL AND oe.metadata::text != 'null' AND NOT (oe.metadata ? 'failure_reason'))
-- )
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset)
FOR UPDATE SKIP LOCKED;

-- name: QueryOutboxEventsUnSentByDestination :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND sqlc.arg(destination) = oe.destination
AND oe.sent_time IS NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventsSent :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.sent_time IS NOT NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryOutboxEventsSentForUpdate :many
SELECT
  sqlc.embed(oe)
FROM outbox_events oe
WHERE sqlc.arg(include_deleted)::bool OR oe.deleted_at IS NULL
AND oe.sent_time IS NOT NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN oe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN oe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN oe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN oe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN oe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN oe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
