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

-- Loads a url and locks its row until the transaction ends, so two writers deciding on its state (two deletes) are
-- serialized and only one records the transition.
-- name: QueryUrlByIdForUpdate :one
SELECT * FROM urls WHERE id = $1 FOR UPDATE;

-- name: QueryDeleteUrlWithId :one
DELETE FROM urls WHERE id = $1 RETURNING *;

-- Persists the state the URL aggregate owns after a transition. deleted_at follows the status, so
-- the DELETED transition releases the target for reuse (idx_urls_original_url is partial on
-- deleted_at IS NULL) and no other status can leave a stale deleted_at behind.
-- name: QueryUpdateUrl :one
UPDATE urls
SET
  status = sqlc.arg(status)::url_status,
  expires_on = sqlc.arg(expires_on),
  deleted_at = CASE WHEN sqlc.arg(status)::url_status = 'DELETED' THEN COALESCE(deleted_at, NOW()) END,
  updated_at = NOW()
WHERE id = sqlc.arg(id) RETURNING *;

-- Claims the next block of short code counter values. short_code_counter increments by the block
-- size, so the caller owns [value, value + block size).
-- name: QueryNextShortCodeBlock :one
SELECT nextval('short_code_counter')::bigint AS block_start;
