---
paths:
  - "backend/**/*.go"
---

# Go backend rules

- Routing: stdlib `net/http` 1.22+ patterns (`mux.HandleFunc("POST /api/tickets", ...)`). No web framework.
- Staff handlers: wrap with `require("<permission>")`. Permission names come from the table in docs/REQUIREMENTS.md. `TestStaffRoutes_FRR3` fails any `/api/staff/` route without it, and any non-GET staff route a Viewer can call.
- Every write that changes state also inserts into `audit_log` inside the same `db.Transaction`.
- A new non-GET route fails `TestAuditCoverage_FRL1` (internal/api/audit_coverage_test.go) until it has a case there that calls it and names its audit action, or an exemption with a reason. A GET route that changes state must be added by hand.
- Return error codes as JSON (`{"error":"ticket.not_found"}`), never translated text.
- Wrap errors with `fmt.Errorf("...: %w", err)`; log with `log/slog`.
- Pass `context.Context` from the request into GORM (`db.WithContext(ctx)`).
- Background goroutines (jobs, workers) recover panics per run and log them: net/http only recovers handlers, so a panic elsewhere kills the server (see the auto-close loop in cmd/ticket-app/main.go).
- Tests: table-driven, name the requirement ID in the test name or a comment.
- Never encode a model straight to a response. `Ticket` and `Comment` refuse `json.Marshal` on purpose; map to a guest or staff response struct that lists exactly the fields the caller may see (FR-G5, NFR-2).
- Audited writes: `db.Transaction(func(tx *gorm.DB) error { ...change...; return audit.Record(tx, actor, action, change) })`. `audit.Record` returns an error if given the pool instead of a transaction.
- GORM logging goes through slog with `ParameterizedQueries: true`; never log SQL with bound values (they carry personal data).
- Guest text input: reject control characters and bidi controls; allow zero-width joiners/spaces inside Burmese and Thai text but never as the whole value; employee IDs allow no format (Cf) characters at all (see `badRune` in internal/api/tickets.go).
- Invisible characters in source: write them as numeric code points (`0x200B`) or build them from char codes in scripts. Tool inputs decode backslash-u escapes into raw invisible characters before the file is written.
- GORM `tx.Model(&t).Updates(map)` writes the new values into `t`. Read any old value you need (for example the status for an audit entry) before calling Updates.
