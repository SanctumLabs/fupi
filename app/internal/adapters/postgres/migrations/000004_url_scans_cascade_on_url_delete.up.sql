BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- A URL that is purged (hard deleted) takes its scan results with it, as it already takes its keywords. Without the
-- cascade, the first scan recorded for a URL would make that URL impossible to purge.
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

ALTER TABLE url_scans DROP CONSTRAINT url_scans_url_id_fkey;
ALTER TABLE url_scans ADD CONSTRAINT url_scans_url_id_fkey FOREIGN KEY (url_id) REFERENCES urls(id) ON DELETE CASCADE;

COMMIT;
