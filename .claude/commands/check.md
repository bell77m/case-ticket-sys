---
description: Run all local checks (Go, frontend, i18n) and report failures only
---

Run whatever exists of:
- `cd backend && go vet ./... && go test -race -cover ./...` (on Windows without gcc, `-race` fails with "requires cgo": drop `-race` locally; CI on Linux keeps it)
- `cd backend && golangci-lint run`
- `cd frontend && npx svelte-check && npm run lint && npm test`
- `node scripts/check-i18n.mjs`

Skip a step if its directory or tool is missing and say so. Report only failures, with the shortest decisive error line.
