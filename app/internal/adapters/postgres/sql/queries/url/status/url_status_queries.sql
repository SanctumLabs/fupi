-- name: QueryCreateUrlStatus :one
INSERT INTO url_status (name, description) VALUES ($1, $2) RETURNING *;

-- name: QueryAllUrlStatuses :many
SELECT
  id,
  name,
  description,
  created_at,
  updated_at,
  deleted_at
FROM url_status us
WHERE CASE 
  WHEN sqlc.arg(include_deleted)::bool=true THEN us.deleted_at IS NULL OR us.deleted_at IS NOT NULL
  WHEN sqlc.arg(include_deleted)::bool=false THEN us.deleted_at IS NULL
  ELSE us.deleted_at IS NULL
END
ORDER BY us.created_at DESC
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryUrlStatusById :one
SELECT
  id,
  name,
  description,
  created_at, 
  updated_at,
  deleted_at
FROM url_status
WHERE id = $1;

-- name: QueryUrlStatusByName :one
SELECT
  id,
  name,
  description,
  created_at, 
  updated_at,
  deleted_at
FROM url_status
WHERE name = $1;

-- name: QueryUpdateUrlStatus :one
UPDATE url_status us
SET
  name=$2,
  description=$3
WHERE us.id = $1 RETURNING *;

-- name: QuerySoftDeleteUrlStatus :one
UPDATE url_status
SET
  deleted_at = $2
WHERE id = $1 RETURNING *;

-- name: QueryDeleteUrlStatusWithId :one
DELETE FROM url_status WHERE id = $1 RETURNING *;
