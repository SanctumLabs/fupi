-- name: QueryCreateKeyword :one
INSERT INTO keywords (
  url_id,
  value
)
VALUES ($1, $2) RETURNING *;

-- name: QueryUpdateKeyword :one
UPDATE keywords
SET
  value=$2,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QuerySoftDeleteKeyword :one
UPDATE keywords
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteKeywordWithId :one
DELETE FROM keywords WHERE id = $1 RETURNING *;
