BEGIN;

------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
-- User roles (ADR-0022). Every user is a 'user'. There is no way to become an 'admin' through the API: promote someone with
--   UPDATE users SET role = 'admin' WHERE email = '...';
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------

CREATE TYPE user_role AS ENUM ('user', 'admin');

ALTER TABLE users ADD COLUMN role user_role NOT NULL DEFAULT 'user';

COMMENT ON COLUMN users.role IS 'User role; only admins may reinstate suspended urls. Set by SQL only.';

COMMIT;
