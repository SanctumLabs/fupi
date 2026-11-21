-- name: QueryCreateUrlScan :one
INSERT INTO url_scans (
  url_id, 
  provider, 
  result, 
  raw_response
)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: QuerySoftDeleteUrlScan :one
UPDATE url_scans
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteUrlScanWithId :one
DELETE FROM url_scans WHERE id = $1 RETURNING *;
