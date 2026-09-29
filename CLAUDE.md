# CLAUDE.md

Guidance for AI coding assistants working in this repo. Read docs/REQUIREMENTS.md and docs/ARCHITECTURE.md before building a feature; tick tasks in docs/PLAN.md when done, then run `/progress` (task note, doc updates, PROGRESS.md).

## Project

Internal IT support ticket system. Guests (employees) open tickets without login; IT staff log in with a username and password and work tickets under RBAC.

## Layout (target)

```
backend/            Go module
  cmd/ticket-app/   main.go (server + create-root-admin command)
  internal/         handlers, models, rbac, audit, auth, reports
  migrations/       goose SQL files
frontend/           SvelteKit app
  messages/         en.json, zh-CN.json, my.json, th.json
deploy/             Kustomize base + overlays (staging, prod)
docs/               requirements, architecture, plan
  notes/            one note per finished task (written by /progress)
```

## Commands

Run from the repo root. Tools are installed per user (Go in `%LOCALAPPDATA%\Programs\go`, make via winget); open a new terminal after installing so PATH picks them up.

- Run locally: `make dev` (Go API on :8080, SvelteKit on http://localhost:5173, which proxies `/api` and `/healthz`)
- Staff sign-in in dev: open /login and sign in as `root`, `agent` or `viewer` with password `dev-password` (from `make seed`). There is no SSO (T2.15).
- First Root Admin: `ticket-app create-root-admin --username <username>`; it prints a temporary password once, changed at first sign-in (FR-A6, FR-A8)
- Test: `make test` (Go tests + i18n key check)
- Lint: `make lint` (golangci-lint + svelte-check)
- Build: `make build` (SvelteKit to `frontend/build`, Go binary to `backend/bin/ticket-app`)
- i18n key check alone: `node scripts/check-i18n.mjs` (fails on missing keys; prints how many values are still English placeholders). `--strict` also fails on placeholders; keys that are correctly the same in every language (such as `#{id}`) go in `sameOK` in that script.
- End-to-end tests: `cd frontend && npm run test:e2e` (Playwright with the installed Edge at phone size; starts the Go API and Vite if they are not running; needs `docker compose up -d --wait && make migrate seed`). Includes axe checks of the guest form, login, forced password change, queue and ticket detail. A Go API left running from before a backend change is reused as is, so stop it first; `go run` can leave `ticket-app.exe` listening on :8080 after Playwright exits (check with `netstat -ano | grep :8080`). The API Playwright starts gets `GUEST_TICKET_LIMIT=1000`; one you start yourself needs that too, or the guest form answers 429 after 5 tickets in 10 minutes (NFR-3). When other sessions work in this folder at the same time, they share the API, Vite and the database: a route one of them adds makes Vite reload every open page, so rerun a failed test alone before counting it as a failure. Gotenberg prints (export.spec) of pages from the Vite dev server sometimes hang for 30 s; against the built app they did not. For a reliable export run, test the built app as CI does: `make serve-built` (Go binary with the built SPA on :8080), then `cd frontend && E2E_BASE_URL=http://localhost:8080 npx playwright test`. Specs use `baseURL` from e2e/helpers.ts, never a hard-coded host.
- Accessibility check (component gallery): run `npm run dev` in frontend, then open http://localhost:5173/dev/components?axe; the page prints axe-core violations. Headless: `msedge --headless=new --user-data-dir=<temp> --lang=<locale> --virtual-time-budget=20000 --dump-dom <url>` (use a fresh profile or the locale cookie leaks between runs)
- Local services: `docker compose up -d --wait` (PostgreSQL :5432, Redis :6379, Gotenberg :3000, all on 127.0.0.1). If `docker info` cannot reach the engine, start Docker Desktop (per-user install: `%LOCALAPPDATA%\Programs\DockerDesktop\Docker Desktop.exe`) and wait for `docker info` to answer; without it `make test` fails on the DB tests.
- PDF export in dev (FR-P4): the Makefile and playwright.config.ts default `GOTENBERG_URL=http://localhost:3000` and `PRINT_BASE_URL=http://host.docker.internal:5173` (the Gotenberg container reaches Vite through Docker Desktop; vite.config.ts binds 127.0.0.1 and allows that host). `make dev` must be running for an export to work.
- Migrate: `make migrate` / `make migrate-down` (runs as owner `ticket` via MIGRATE_DATABASE_URL; the app connects as `ticket_app`)
- Seed dev data: `make seed` (sample locations and dev staff; safe to run twice). Default roles and categories come from migrations, not seed.
- DB tests: `make test` runs them when DATABASE_URL and REDIS_URL are set (the Makefile sets them from .env.example); plain `go test` skips them silently. One package or test: `make test-go PKG=./internal/api RUN=Queue`
- Docker image: `docker build -t ticket-app:dev .` (under 40 MB; the woff2-only plugin in vite.config.ts keeps legacy .woff fonts out). Migration image: `docker build -f deploy/migrate.Dockerfile -t ticket-migrate:dev .` (goose + backend/migrations, same tag as the app)
- Kubernetes manifests: `make deploy-lint` renders deploy/overlays/{staging,prod} with `kubectl kustomize` and runs kube-linter and Trivy config from Docker images (needs Docker running)
- Local test cluster (k3s in Docker via k3d; tools from winget: `k3d.k3d`, `Helm.Helm`): `make cluster-up` (cluster `ticket-local`, context `k3d-ticket-local`, host ports 127.0.0.1:8088/8443; also runs `make cluster-platform`, which installs the platform pieces from deploy/platform), `make cluster-deploy` (builds both images, imports them and the third-party images, applies deploy/overlays/local in order: data, migration Job, app), `make cluster-seed` (dev locations and staff), `make cluster-check-ingress` (the T3.10 checks, deploy/local/ingress-check.sh), `make cluster-down`. The app answers at https://tickets.localtest.me:8443 (trust deploy/overlays/local/tls/ca.crt; Windows curl also needs `--ssl-no-revoke`). Passwords and the TLS key are random, in the gitignored deploy/overlays/local/secrets.env and tls/. For testing the platform tasks only; never real data. In Git Bash the winget package folders may be missing from PATH: add `$(cygpath "$LOCALAPPDATA")/Microsoft/WinGet/Packages/k3d.k3d_Microsoft.Winget.Source_8wekyb3d8bbwe` and the Helm `windows-amd64` folder.
- CI: .github/workflows/ci.yml (GitHub Actions, every PR and push to main): tests with coverage gate 70% (`-coverpkg=./...`), lint, i18n, the full Playwright suite against the built app (Gotenberg as a container, one retry in CI), Gitleaks, Semgrep, govulncheck, Trivy
- CD: .github/workflows/cd.yml (after CI passes on a push to main): builds the app and migration images, Trivy image scan, pushes to GHCR (`ghcr.io/<owner>/<repo>/ticket-app`, `.../ticket-migrate`), Syft SBOMs, Cosign keyless sign and attest, then commits the new digests to deploy/overlays/staging (`[skip ci]`). Interim until Harbor (T3.12) and Vault (T3.11); prod is promoted by hand. Lint workflows with `docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:latest` (Git Bash: set `MSYS_NO_PATHCONV=1`).

## Claude Code setup

- Subagents (`.claude/agents/`): `go-backend`, `svelte-frontend`, `security-reviewer` (read-only), `i18n-translator`. For long jobs (such as translating a whole message file) tell the agent to edit in batches of about 50 keys, so a stop loses little. A subagent stopped by a model's session limit can be rerun at once with the Agent tool's `model` set to another model.
- Commands (`.claude/commands/`): `/next-task`, `/auto-task`, `/req <ID>`, `/progress`, `/check`.
- Hooks (`.claude/settings.json`): after an Edit ticks a task in docs/PLAN.md, `.claude/hooks/task-done.mjs` tells Claude to run `/progress`.
- Path rules (`.claude/rules/`): Go backend, migrations, frontend, deploy. They load when matching files are touched.
- Skills (`.claude/skills/`): Go skills from github.com/cxuu/golang-skills (Apache-2.0). Svelte skills, MCP and LSP come from the `svelte` plugin.
- Plugins (`.claude/settings.json`): svelte, gopls-lsp, security-guidance, commit-commands, playwright.
- MCP (`.mcp.json`): `postgres-dev` for the local docker-compose database; set `DEV_DATABASE_URL` to override. Never point it at staging or prod.

## Work loop

Every task runs the same loop. `/next-task` drives it for one task. `/loop /auto-task` runs it unattended: one task per pass, a fresh recheck before each tick, blocked tasks recorded in PROGRESS.md and skipped.

1. **Pick.** Take one unchecked task from docs/PLAN.md. Read its requirement IDs in docs/REQUIREMENTS.md and its done criteria.
2. **Plan.** State in 3 lines what changes and which files. Stop and ask if a requirement is unclear or an open question blocks it.
3. **Test first.** Write the failing test named after the requirement ID (for example `TestGuestCreate_FRG1`). Run it and see it fail.
4. **Build.** Write the smallest change that makes the test pass.
5. **Check.** Run `/check`. Fix every failure before going on.
6. **Review.** Run the security-reviewer agent when the change touches auth, RBAC, audit, guest endpoints or uploads. Fix its findings, then go back to step 5. Give it a short list of files (4 or fewer) and a word limit: broad prompts stall, narrow ones finish in about a minute. A wide review (such as T2.14) is several such passes, one per area, run in parallel. If the agent stalls twice, review inline against the same checklist and record that in PROGRESS.md.
7. **Close.** Confirm each done criterion with evidence (command output, not memory). Tick the task in docs/PLAN.md and run `/progress`: it writes the task note in docs/notes/, updates the docs the task made stale and updates PROGRESS.md. A hook (`.claude/hooks/task-done.mjs`) reminds you when a tick lands.
8. **Learn.** If the loop hit a surprise (bad rule, missing command, wrong assumption), fix CLAUDE.md, a rule file or docs/PLAN.md now, so the next loop does not repeat it.

Exit the loop early and report when a check fails 3 times on the same cause, or when a task needs a decision only a human can make. `/auto-task` records such a task under Blockers and moves to the next ready one instead.

## Rules

- **Permissions on the server.** Every staff endpoint goes through the RBAC middleware (`require("<permission>")`). Never rely on the UI to hide actions.
- **Root Admin only** can create staff and manage roles. `staff.create` and `role.manage` must not be assignable to any other role.
- **Audit every action** in the same DB transaction as the change. `audit_log` is append-only; never write UPDATE or DELETE against it.
- **Guest data:** never return internal notes on guest endpoints. Tracking tokens are stored only as SHA-256 hashes.
- **Migrations** are goose SQL files. Never enable GORM AutoMigrate in production code.
- **i18n:** no hard-coded user-facing text in the frontend. Add every key to all four message files (en, zh-CN, my, th); use English as a placeholder if a translation is missing. The API returns error codes, not sentences.
- **Enums** (status, priority, permission) are stored as codes, translated only in the frontend.
- **Secrets** come from environment variables (filled by Vault via VSO). Never commit secrets or put them in images.
- **Uploads:** check type from file content, enforce size limits in the API.
- **UI follows [DESIGN.md](DESIGN.md).** Use its tokens (CSS variables in `frontend/src/lib/styles/tokens.css`) and component specs; no hard-coded colors, sizes or radii. Its "App adaptation" section overrides the reference system above it. Never use Cal.com's font, logo or name.
- **Stdlib first** in Go (`net/http`, `embed`, `net/smtp`). Ask before adding a new dependency.
- Refer to requirement IDs (for example FR-L1) in commit messages and tests.
