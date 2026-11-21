-- name: QueryCreateWebhook :one
INSERT INTO webhooks (
  user_id,
  url_id, 
  endpoint, 
  secret, 
  events
)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: QueryUpdateWebhookEvent :one
UPDATE webhooks
SET
  events = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateWebhookEndpoint :one
UPDATE webhooks
SET
  endpoint = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateWebhookSecret :one
UPDATE webhooks
SET
  secret = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryUpdateWebhookActive :one
UPDATE webhooks
SET
  active = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QuerySoftDeleteWebhook :one
UPDATE webhooks
SET
  deleted_at = $2,
  updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: QueryDeleteWebhookWithId :one
DELETE FROM webhooks WHERE id = $1 RETURNING *;
