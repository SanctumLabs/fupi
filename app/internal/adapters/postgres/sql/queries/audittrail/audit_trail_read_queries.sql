-- name: QueryAllAuditTrails :many
SELECT
  sqlc.embed(t),
  COUNT(*) OVER() AS total_records
FROM audit_trail t
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllAuditTrailsByTable :many
SELECT 
  sqlc.embed(t),
  COUNT(*) OVER() AS total_records  
FROM audit_trail t
WHERE t.table_name = sqlc.arg(table_name)
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllAuditTrailsById :many
SELECT 
  sqlc.embed(t),
  COUNT(*) OVER() AS total_records
FROM audit_trail t
WHERE t.id = sqlc.arg(id)
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);

-- name: QueryAllAuditTrailsByAction :many
SELECT 
  sqlc.embed(t),
  COUNT(*) OVER() AS total_records  
FROM audit_trail t
WHERE t.action = sqlc.arg(action)
LIMIT sqlc.arg(limit_by)
OFFSET sqlc.arg(current_offset);
