-- name: QueryAllKafkaOutboxEvents :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN koe.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN koe.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN koe.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllKafkaOutboxEventsCount :one
SELECT
  count(*)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL;

-- name: QueryKafkaOutboxEventsByPartitionKey :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
AND koe.partition_key = sqlc.arg(partition_key)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN b.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN b.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN b.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryKafkaOutboxEventByCorrelationId :one
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE koe.correlation_id = sqlc.arg(correlation_id);

-- name: QueryKafkaOutboxEventById :one
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE koe.id = $1;

-- name: QueryKafkaOutboxEventsByDestination :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
AND koe.destination = $1
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN b.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN b.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN b.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryKafkaOutboxEventsByEventType :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
AND koe.event_type = $1
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN b.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN b.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN b.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryKafkaOutboxEventsUnSent :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
AND koe.sent_time IS NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN b.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN b.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN b.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryKafkaOutboxEventsSent :many
SELECT
  sqlc.embed(koe)
FROM kafka_outbox_events koe
WHERE sqlc.arg(include_deleted)::bool OR koe.deleted_at IS NULL
AND koe.sent_time IS NOT NULL
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN b.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN b.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN b.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  CASE 
    WHEN sqlc.arg(order_by)::text = 'created_at' THEN koe.created_at 
    WHEN sqlc.arg(order_by)::text = 'updated_at' THEN koe.updated_at 
    WHEN sqlc.arg(order_by)::text = 'deleted_at' THEN koe.deleted_at 
END,
  CASE
    WHEN sqlc.arg(sort_order)::text='ASC' THEN 'ASC'
    ELSE 'DESC'
END
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
