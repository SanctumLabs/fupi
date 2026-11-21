-- name: QueryUserById :one
SELECT 
  sqlc.embed(u),
  sqlc.embed(us)
FROM users u
JOIN user_status us ON u.status_id = us.id 
WHERE u.id = $1;

-- name: QueryUserByUsername :one
SELECT 
  sqlc.embed(u), 
  sqlc.embed(us) 
FROM users u 
JOIN user_status us ON u.status_id = us.id
WHERE u.username = $1;

-- name: QueryUserByEmail :one
SELECT 
  sqlc.embed(u), 
  sqlc.embed(us) 
FROM users u
JOIN user_status us ON u.status_id = us.id
WHERE u.email = $1;

-- name: QueryAllUsers :many
SELECT 
  sqlc.embed(u),
  sqlc.embed(us),
  COUNT(*) OVER() AS total_records
FROM users u 
JOIN user_status us ON u.status_id = us.id
WHERE sqlc.arg(include_deleted)::bool OR u.deleted_at IS NULL
  AND (COALESCE(sqlc.narg(user_status), '') = '' OR us.name = sqlc.narg(user_status))
-- Date range filtering
AND (
  sqlc.narg(date_field)::text IS NULL
  OR sqlc.narg(date_from)::timestamp IS NULL
  OR sqlc.narg(date_to)::timestamp IS NULL
  OR (
    CASE 
      WHEN sqlc.narg(date_field)::text = 'created_at' THEN u.created_at
      WHEN sqlc.narg(date_field)::text = 'updated_at' THEN u.updated_at
      WHEN sqlc.narg(date_field)::text = 'deleted_at' THEN u.deleted_at
    END BETWEEN sqlc.narg(date_from)::timestamp AND sqlc.narg(date_to)::timestamp
  )
)
ORDER BY
  -- Sort by 'created_at'
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN u.created_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'created_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN u.created_at END DESC,
  
  -- Sort by 'updated_at'
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN u.updated_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'updated_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN u.updated_at END DESC,
  
  -- Sort by 'deleted_at'
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'ASC' 
    THEN u.deleted_at END ASC,
  CASE WHEN sqlc.arg(order_by)::text = 'deleted_at' AND sqlc.arg(sort_order)::text = 'DESC' 
    THEN u.deleted_at END DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
