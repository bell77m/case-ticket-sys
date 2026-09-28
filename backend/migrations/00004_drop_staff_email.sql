-- Staff have no email: notifications are in-app only (decided 2026-09-24, T2.12 skipped), so the app never uses it.
-- Down brings the column back empty; the addresses themselves are gone.

-- +goose Up
DROP INDEX staff_email_key;
ALTER TABLE staff DROP COLUMN email;

-- +goose Down
ALTER TABLE staff ADD COLUMN email text;
CREATE UNIQUE INDEX staff_email_key ON staff (lower(email));
