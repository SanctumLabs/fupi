BEGIN;

ALTER TABLE url_scans DROP CONSTRAINT url_scans_url_id_fkey;
ALTER TABLE url_scans ADD CONSTRAINT url_scans_url_id_fkey FOREIGN KEY (url_id) REFERENCES urls(id);

COMMIT;
