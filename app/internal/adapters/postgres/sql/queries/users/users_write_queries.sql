-- name: QueryCreateUser :one
INSERT INTO users (username, first_name, last_name, email, password_hash, status_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: QueryUpdateUserDetails :one
UPDATE users
SET
  username=$2,
  first_name=$3,
  last_name=$4,
  email=$5,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateUserVerification :one
UPDATE users
SET
  verified=$2,
  verification_token=$3,
  verification_expires=$4,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateUserMetadata :one
UPDATE users SET metadata=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUserPassword :one
UPDATE users SET password_hash=$2, updated_at=NOW() WHERE id = $1 RETURNING *;

-- name: QueryUpdateUserStatusId :one
UPDATE users
SET
  status_id=$2,
  updated_at=NOW()
WHERE id = $1 RETURNING *;

-- name: QuerySoftDeleteUser :one
UPDATE users
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteUserWithId :one
DELETE FROM users WHERE id = $1 RETURNING *;
