# 2026-09-24-drop-staff-email Drop staff email and SMTP settings

Date: 2026-09-24 · Requirements: FR-A1, FR-A6 · Agent: be + fe

## What was done
- Following up on T2.12 (in-app only), the user decided that staff have no email. Migration 00004 drops `staff.email` and its unique index.
- Root Admin creates staff with a name, username, role and temporary password only. The API rejects an `email` field (400 `invalid_body`), and the `staff.email_taken` error is gone. `create-root-admin` no longer takes `--email`.
- The staff admin page has no email field or column; cards show the username.
- The unused SMTP settings (SMTP_ADDR, SMTP_FROM, SMTP_USER, SMTP_PASSWORD) are no longer read.

## Files
- backend/migrations/00004_drop_staff_email.sql: drops the column and index. Down restores an empty nullable column.
- backend/internal/models/models.go: Staff has no Email.
- backend/internal/api/staff.go: create input, list output and query without email. A conflict can only be the username, so it always means `staff.username_taken`.
- backend/cmd/ticket-app/rootadmin.go: no `--email` flag; the taken check is by username only.
- backend/internal/config/config.go: SMTP fields and the SMTP_FROM check removed.
- backend/seed/dev.sql: dev staff without email; upsert on username.
- Tests:
  - migrations_test.go: TestStaffHasNoEmail_FRA1
  - config_test.go: TestLoad_NoSMTP, and the SMTP case removed
  - rootadmin_test.go: `--email` is now a bad argument
  - staff_test.go: email rows replaced by an "email field" row expecting 400
  - auth_test.go: newStaff without email
- frontend/src/lib/api.ts: StaffAccount and createStaffAccount without email.
- frontend/src/routes/staff/admin/staff/+page.svelte: email field, column and error handling removed; the card shows the username.
- frontend/messages/*.json: staff_email, staff_no_email, staff_err_email_taken and staff_err_email_invalid removed from all four files.
- frontend/e2e/admin.spec.ts: rows found by username text; the form has no email field.

## Decisions
- None made by the agent. The user chose both the SMTP cleanup and dropping staff email (2026-09-24).

## Checks
- Each new test failed first:
  - TestStaffHasNoEmail_FRA1: "staff.email columns = 1"
  - TestLoad_NoSMTP: "missing environment variables: SMTP_FROM"
  - TestStaffCreate_FRA1/email_field: got 201
  - the root-admin bad-arguments test: did not compile against the old signature
- All of them pass now.
- Migration 00004: up, down and up clean. `make seed` runs twice without error.
- `make test`: exit 0 (uncached). `make lint`: 0 issues, svelte-check 0 errors. `node scripts/check-i18n.mjs` passes.
- `npm run test:e2e`: 27/27. One earlier run timed out once in layout.spec.ts (queue page wait, untouched by this change); it passed twice on its own and in the next full run.
- security-reviewer: no findings.

## Docs updated
- docs/REQUIREMENTS.md FR-A1: "Staff have no email (notifications are in-app only)" replaces "optional work email for notifications" (user decision).
- docs/ARCHITECTURE.md: the staff table no longer lists email.
- DESIGN.md "Staff accounts": no email field or column; cards show the username.
- CLAUDE.md: `create-root-admin` without `[--email]`.

## Follow-ups
- .env.example still has the SMTP_ADDR, SMTP_FROM, SMTP_USER and SMTP_PASSWORD lines, and the OIDC_* lines from T2.15. Delete them by hand (agents cannot edit that file). Nothing reads them.
- layout.spec.ts "queue summary is readable at tablet widths" makes 8 page loads within the 30 s test timeout and timed out once under full-suite load. If it happens again, give it `test.slow()`.
