-- name: QueryCreateUrl :one
INSERT INTO urls (
  user_id, 
  short_code, 
  custom_alias, 
  original_url, 
  status_id, 
  expires_on, 
  og_title, 
  og_description, 
  og_image_url,
  metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *;

-- name: QueryUpdateUrlDetails :one
UPDATE urls
SET
  short_code=$2,
  custom_alias=$3,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateUrlMetadata :one
UPDATE urls SET metadata=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUrlExpiresOn :one
UPDATE urls SET expires_on=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUrlStatusId :one
UPDATE urls
SET
  status_id=$2,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QuerySoftDeleteUrl :one
UPDATE urls
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteUrlWithId :one
DELETE FROM urls WHERE id = $1 RETURNING *;
