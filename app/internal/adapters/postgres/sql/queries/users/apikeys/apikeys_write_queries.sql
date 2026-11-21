-- name: QueryCreateApiKey :one
INSERT INTO api_keys (user_id, key_hash, name, scopes, rate_limit, expires_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: QueryUpdateApiKey :one
UPDATE api_keys ak
SET
  last_used_at=$2,
  expires_at=$3,
  updated_at=NOW()
WHERE ak.id = $1 RETURNING *;

-- name: QueryUpdateApiKeyLastUsed :one
UPDATE api_keys ak
SET
  last_used_at=$2,
  updated_at=NOW()
WHERE ak.id = $1 RETURNING *;

-- name: QuerySoftDeleteApiKey :one
UPDATE api_keys
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteApiKeyWithId :one
DELETE FROM api_keys WHERE id = $1 RETURNING *;
