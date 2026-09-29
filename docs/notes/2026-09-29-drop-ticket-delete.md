# 2026-09-29-drop-ticket-delete Remove the unused ticket.delete permission

Date: 2026-09-29 · Requirements: FR-R2, FR-L1 · Agent: be (main agent)

## What was done
- Tickets are never deleted. The `ticket.delete` permission is gone from the code, the database and the roles page. No feature ever used it (the question from T3.06).
- Migration 00005:
  - takes the permission away from the roles that had it (Root Admin and Admin);
  - writes one `role.changed` row per role to the activity log, as actor "system", with `ticket.delete` as the removed permission;
  - removes it from the CHECK list, so it can no longer be granted.
- `rbac.All` has 10 permissions. The roles API refuses `ticket.delete` as an unknown permission, and the roles page no longer shows it.

## Files
- backend/migrations/00005_drop_ticket_delete.sql (new): the change and its down step (which grants it back to Root Admin and Admin).
- backend/internal/rbac/rbac.go: `TicketDelete` removed.
- backend/migrations/migrations_test.go: `TestTicketDeleteDropped_FRR2`; Admin's default permissions updated.
- backend/internal/api/rbac_test.go: Admin's default permissions updated.
- frontend/src/lib/permissions.ts, activity.ts: the label and the `ticket.deleted` action removed.
- frontend/messages/{en,zh-CN,my,th}.json: `perm_ticket_delete` and `activity_action_ticket_deleted` removed.
- docs/REQUIREMENTS.md: see Docs updated.

## Decisions
- The user chose on 2026-09-29 to drop the permission instead of building deletion. To build deletion later: a new migration adds the permission back to the CHECK list, plus the feature, the `ticket.deleted` audit action and its label.

## Checks
- Test first: `TestTicketDeleteDropped_FRR2` and `TestDefaultRoles_FRR2` failed before the migration (2 roles held it, nothing audited, the insert was accepted).
- `make migrate`, `make migrate-down`, `make migrate` on the compose DB:
  - the down step gave the permission back to Root Admin and Admin;
  - up again: both tests pass.
- `make test-go PKG=./...`: all packages ok.
- `check-i18n` exit 0 and svelte-check 0 errors.
- e2e: in `admin.spec.ts`, the role-editor test now ticks "View reports". It got past the tick and the "Unsaved changes" check, then failed at the axe step together with 2 other tests. The cause is a CSP change another session has in progress (`page.addScriptTag` blocked by `script-src`), not this change.

## Docs updated
- docs/REQUIREMENTS.md: the `ticket.delete` row and the `ticket.deleted` action are removed, with one line saying tickets are never deleted and why.

## Follow-ups
- None.
