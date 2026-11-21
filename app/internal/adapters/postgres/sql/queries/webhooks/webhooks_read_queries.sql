-- name: QueryWebhookById :one
SELECT 
  sqlc.embed(wh)
FROM webhooks wh
WHERE wh.id = $1;

-- name: QueryWebhookByUserId :many
SELECT 
  sqlc.embed(wh)
FROM webhooks wh
WHERE wh.user_id = $1
AND sqlc.arg(include_deleted)::bool OR wh.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(active), '') = '' OR wh.active = sqlc.narg(active)::bool)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN wh.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN wh.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN wh.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllWebhooks :many
SELECT 
  sqlc.embed(wh),
  COUNT(*) OVER() AS total_records
FROM webhooks wh 
WHERE sqlc.arg(include_deleted)::bool OR wh.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(active), '') = '' OR wh.active = sqlc.narg(active)::bool)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN wh.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN wh.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN wh.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllWebhooksByUrlId :many
SELECT 
  sqlc.embed(wh),
  sqlc.embed(u),
  COUNT(*) OVER() AS total_records
FROM webhooks wh 
JOIN urls u ON wh.url_id = u.id
WHERE wh.url_id = $1
  AND (sqlc.arg(include_deleted)::bool OR wh.deleted_at IS NULL)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN wh.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN wh.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN wh.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN wh.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN wh.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
