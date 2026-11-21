-- name: QueryCreateUserStatus :one
INSERT INTO user_status (name, description) VALUES ($1, $2) RETURNING *;

-- name: QueryAllUserStatuses :many
SELECT
  id,
  name,
  description,
  created_at,
  updated_at,
  deleted_at,
  COUNT(*) OVER() AS total_records
FROM user_status us
WHERE CASE 
  WHEN sqlc.arg(include_deleted)::bool=true THEN us.deleted_at IS NULL OR us.deleted_at IS NOT NULL
  WHEN sqlc.arg(include_deleted)::bool=false THEN us.deleted_at IS NULL
  ELSE us.deleted_at IS NULL
END
ORDER BY us.created_at DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryUserStatusById :one
SELECT
  id,
  name,
  description,
  created_at, 
  updated_at,
  deleted_at
FROM user_status
WHERE id = $1;

-- name: QueryUserStatusByName :one
SELECT
  id,
  name,
  description,
  created_at, 
  updated_at,
  deleted_at
FROM user_status
WHERE name = $1;

-- name: QueryUpdateUserStatus :one
UPDATE user_status us
SET
  name=$2,
  description=$3
WHERE us.id = $1 RETURNING *;

-- name: QuerySoftDeleteUserStatus :one
UPDATE user_status
SET
  deleted_at = $2
WHERE id = $1 RETURNING *;

-- name: QueryDeleteUserStatusWithId :one
DELETE FROM user_status WHERE id = $1 RETURNING *;
