-- name: QueryKeywordById :one
SELECT 
  sqlc.embed(kw)
FROM keywords kw
WHERE kw.id = $1;

-- name: QueryKeywordByValue :one
SELECT 
  sqlc.embed(kw)
FROM keywords kw
WHERE kw.value = $1 AND kw.url_id = $2;

-- name: QueryAllKeywords :many
SELECT 
  sqlc.embed(kw),
  COUNT(*) OVER() AS total_records
FROM keywords kw 
WHERE sqlc.arg(include_deleted)::bool OR kw.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(url_status), '') = '' OR us.name = sqlc.narg(url_status))
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN kw.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN kw.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN kw.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllKeywordsByUrlId :many
SELECT 
  sqlc.embed(kw),
  sqlc.embed(u),
  COUNT(*) OVER() AS total_records
FROM keywords kw 
JOIN urls u ON kw.url_id = u.id
WHERE kw.url_id = $1
  AND (sqlc.arg(include_deleted)::bool OR kw.deleted_at IS NULL)
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN kw.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN kw.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN kw.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN kw.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN kw.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
