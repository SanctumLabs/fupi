-- name: QueryCreateUrl :one
INSERT INTO urls (
  id,
  user_id, 
  short_code, 
  custom_alias, 
  original_url, 
  status, 
  expires_on, 
  og_title, 
  og_description, 
  og_image_url,
  metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING *;

-- name: QueryUpdateUrlMetadata :one
UPDATE urls SET metadata=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUrlExpiresOn :one
UPDATE urls SET expires_on=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUrlStatus :one
UPDATE urls
SET
  status=$2,
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
