-- name: QueryUrlScanById :one
SELECT 
  sqlc.embed(us)
FROM url_scans us
WHERE us.id = $1;

-- name: QueryUrlScanByProvider :many
SELECT 
  sqlc.embed(us)
FROM url_scans us
WHERE us.provider = $1
AND sqlc.arg(include_deleted)::bool OR us.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(url_status), '') = '' OR us.name = sqlc.narg(url_status))
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN us.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN us.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN us.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllUrlScans :many
SELECT 
  sqlc.embed(us),
  COUNT(*) OVER() AS total_records
FROM url_scans us 
WHERE sqlc.arg(include_deleted)::bool OR us.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(url_status), '') = '' OR us.name = sqlc.narg(url_status))
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN us.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN us.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN us.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllUrlScansByUrlId :many
SELECT 
  sqlc.embed(us),
  sqlc.embed(u),
  COUNT(*) OVER() AS total_records
FROM url_scans us 
JOIN urls u ON us.url_id = u.id
WHERE us.url_id = $1
  AND (sqlc.arg(include_deleted)::bool OR us.deleted_at IS NULL)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN us.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN us.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN us.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN us.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN us.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
