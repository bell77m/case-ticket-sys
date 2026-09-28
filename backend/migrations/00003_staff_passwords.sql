-- Staff sign in with a username and password instead of SSO (T2.15: FR-R1, FR-A1, FR-A7 to FR-A10).
-- Email stays, optional, for notifications only (T2.12).

-- +goose Up
ALTER TABLE staff
    ADD COLUMN username             text,
    ADD COLUMN password_hash        text        NOT NULL DEFAULT '', -- '' = no password yet: sign-in always fails
    ADD COLUMN must_change_password boolean     NOT NULL DEFAULT true,
    ADD COLUMN password_changed_at  timestamptz NOT NULL DEFAULT now(),
    ALTER COLUMN email DROP NOT NULL;
-- Existing accounts get their email as username and no password; Root Admin resets them (FR-A9).
-- An email with other characters (such as '+') fails the CHECK below: fix that row by hand, then migrate again.
UPDATE staff SET username = lower(email);
ALTER TABLE staff
    ALTER COLUMN username SET NOT NULL,
    ADD CONSTRAINT staff_username_format CHECK (username ~ '^[a-z0-9._@-]{3,64}$');
CREATE UNIQUE INDEX staff_username_key ON staff (username);

-- +goose Down
DROP INDEX staff_username_key;
UPDATE staff SET email = username || '@invalid' WHERE email IS NULL;
ALTER TABLE staff
    DROP COLUMN username,
    DROP COLUMN password_hash,
    DROP COLUMN must_change_password,
    DROP COLUMN password_changed_at,
    ALTER COLUMN email SET NOT NULL;
