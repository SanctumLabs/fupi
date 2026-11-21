BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- Enforce that a given target URL is shortened only once, across all users.
--
-- The index is partial on deleted_at IS NULL so that soft-deleting a URL releases its target for
-- reuse; a plain unique index would burn the target permanently.
--
-- original_url is stored in the canonical form produced by the OriginalURL value object (lowercased
-- scheme and host, no default port, no trailing slash, no utm_* parameters), so this index cannot be
-- bypassed by respelling the same target.
--
-- NOTE: this will fail if the table already holds duplicate original_url values among non-deleted
-- rows. Deduplicate before applying it to a database that has such rows.
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

CREATE UNIQUE INDEX idx_urls_original_url ON urls (original_url) WHERE deleted_at IS NULL;

COMMENT ON INDEX idx_urls_original_url IS 'Enforces one shortened URL per target across all users; partial so soft-deleted rows release their target.';

COMMIT;
