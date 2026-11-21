-- name: QueryApiKeyById :one
SELECT
  sqlc.embed(ak)
FROM api_keys ak
WHERE ak.id = $1;

-- name: QueryAllApiKeys :many
SELECT
  sqlc.embed(ak),
  COUNT(*) OVER() AS total_records
FROM api_keys ak
WHERE CASE 
  WHEN sqlc.arg(include_deleted)::bool=true THEN ak.deleted_at IS NULL OR ak.deleted_at IS NOT NULL
  WHEN sqlc.arg(include_deleted)::bool=false THEN ak.deleted_at IS NULL
  ELSE ak.deleted_at IS NULL
END
ORDER BY ak.created_at DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryApiKeysByUser :many
SELECT
  sqlc.embed(ak),
  COUNT(*) OVER() AS total_records
FROM api_keys ak
WHERE sqlc.arg(include_deleted)::bool OR ak.deleted_at IS NULL
AND ak.user_id = $1
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN ak.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN ak.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN ak.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN ak.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN ak.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN ak.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN ak.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN ak.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN ak.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryApiKeysByUserCount :one
SELECT
  COUNT(*)
FROM api_keys ak
WHERE ak.user_id = $1
AND (sqlc.arg(include_deleted)::bool OR ak.deleted_at IS NULL);
