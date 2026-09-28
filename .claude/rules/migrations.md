---
paths:
  - "backend/migrations/**"
---

# Migration rules

- goose SQL files only: `-- +goose Up` / `-- +goose Down`. Never edit a migration that has been merged; add a new one.
- `audit_log`: grant the app user INSERT and SELECT only (FR-L2).
- Admin-entered names (categories, locations) are JSONB with keys en, zh-CN, my, th (FR-I4).
- Store enums (status, priority, permission) as text codes, with a CHECK constraint.
- Tables are owned by `ticket`; the app role `ticket_app` gets explicit GRANTs in the migration that creates each table. New tables need their GRANT too, or the app gets "permission denied".
- The dev role script (deploy/dev/init-app-user.sql) runs only on a fresh volume. After changing it: `docker compose down -v && docker compose up -d --wait && make migrate` (wipes local data).
- Grant the least each table needs. `comments` has no UPDATE (internal notes must never become public); `audit_log` has no UPDATE or DELETE.
- Down steps use `IF EXISTS` for functions and extensions, so a dev DB migrated from an earlier draft can still roll back.
- `$$` is eaten twice over in one-liners: bash expands it, and JavaScript `String.replace()` turns `$$` into `$`. Write migrations and Makefiles with the Write/Edit tools, then check every `$$`.
