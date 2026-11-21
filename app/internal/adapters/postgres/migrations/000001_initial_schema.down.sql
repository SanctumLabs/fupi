BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Remove audit triggers (must come first, before dropping any audited tables)
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

DROP TRIGGER IF EXISTS audit_trigger_row ON keywords;
DROP TRIGGER IF EXISTS audit_trigger_stm ON keywords;

DROP TRIGGER IF EXISTS audit_trigger_row ON api_keys;
DROP TRIGGER IF EXISTS audit_trigger_stm ON api_keys;

DROP TRIGGER IF EXISTS audit_trigger_row ON webhooks;
DROP TRIGGER IF EXISTS audit_trigger_stm ON webhooks;

DROP TRIGGER IF EXISTS audit_trigger_row ON url_scans;
DROP TRIGGER IF EXISTS audit_trigger_stm ON url_scans;

DROP TRIGGER IF EXISTS audit_trigger_row ON urls;
DROP TRIGGER IF EXISTS audit_trigger_stm ON urls;

DROP TRIGGER IF EXISTS audit_trigger_row ON users;
DROP TRIGGER IF EXISTS audit_trigger_stm ON users;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Drop tables (reverse dependency / FK order — children before parents)
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

-- Eventing context
DROP TABLE IF EXISTS kafka_outbox_events;
DROP TABLE IF EXISTS outbox_events;

-- Security context (url_scans → urls)
DROP TABLE IF EXISTS url_scans;

-- Notification context (webhooks → users, urls)
DROP TABLE IF EXISTS webhooks;

-- URL context (keywords → urls; urls → users, url_status)
DROP TABLE IF EXISTS keywords;
DROP TABLE IF EXISTS urls;
DROP TABLE IF EXISTS url_status;

-- Identity context (api_keys → users; users → user_status)
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS user_status;

-- Audit context (no FK dependencies from other tables; safe to drop last)
DROP TABLE IF EXISTS audit_trail;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Drop sequences
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

DROP SEQUENCE IF EXISTS short_code_counter;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Drop functions (reverse creation order; wrappers before the core function they depend on)
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

-- audit_table() convenience wrappers first
DROP FUNCTION IF EXISTS audit_table(regclass);
DROP FUNCTION IF EXISTS audit_table(regclass, boolean, boolean);
DROP FUNCTION IF EXISTS audit_table(regclass, boolean, boolean, text[]);

-- Core trigger function (referenced by the wrappers above)
DROP FUNCTION IF EXISTS create_log_on_modify();

-- UUID helper
DROP FUNCTION IF EXISTS generate_uuid();

COMMIT;