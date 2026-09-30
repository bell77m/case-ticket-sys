# Architecture

## Tech stack

| Need | Choice | Notes |
| --- | --- | --- |
| Frontend | SvelteKit, adapter-static (SPA) | Built to static files, embedded in the Go binary with `embed` |
| Styling | CSS custom properties from [DESIGN.md](../DESIGN.md) + Svelte scoped styles | No CSS framework |
| Charts | Chart.js | |
| i18n | Paraglide JS | `messages/{en,zh-CN,my,th}.json` |
| Burmese input | myanmar-tools 1.1.3 (pinned: 1.2.0 on npm does not load) | `call()` in `frontend/src/lib/api.ts` converts Zawgyi strings in every JSON write to Unicode (FR-I8); loaded only when a body holds Burmese |
| Backend | Go, stdlib `net/http` (1.22+ routing) | No web framework |
| ORM | GORM + PostgreSQL driver (pgx) | AutoMigrate off in production |
| Migrations | goose | Versioned SQL files |
| Database | PostgreSQL 16 | `pg_trgm` for multilingual search |
| Cache / queue | Redis 7, go-redis | Sessions, rate limits, pub/sub for SSE |
| Realtime | Server-Sent Events + Redis pub/sub | `/api/events` (ticket.view_all). Package `internal/events`: every ticket change publishes `{type, id}` on Redis channel `ticket-events` after commit (no content); each SSE client subscribes, gets a `: ping` every 25 s and is re-authorized at each ping. Clients refetch through the normal endpoints. |
| PDF | Gotenberg (headless Chromium) | Renders the print pages for the report and the activity log: `POST /api/staff/reports/export` or `/api/staff/activity/export` stores the filters under a 60 s one-time token in Redis (key `print:<kind>:<sha256>`, kind report or activity) and asks Gotenberg (`GOTENBERG_URL`) to print `PRINT_BASE_URL/print/<kind>#<token>`; the page reads the data once from `GET /api/print/<kind>` (X-Print-Token) and sets `window.printReady`. Both env vars are optional; without them export answers 503. |
| Auth | Username and password, PBKDF2-SHA256 (Go standard library) | Staff only; no SSO; session ID in HttpOnly cookie, data in Redis |
| Notifications | In-app only | Staff see new and assigned tickets in the queue; no email (decided 2026-09-24) |
| Secrets | HashiCorp Vault + Vault Secrets Operator | App reads env vars; no Vault code in app |

## Data model

| Table | Key columns |
| --- | --- |
| staff | id, name, username, password_hash, must_change_password, password_changed_at, role_id, language, is_active |
| roles | id, name |
| role_permissions | role_id, permission |
| tickets | id, summary, case_details, category_id, priority (null until triage), status, guest_name, employee_id, language, location_id, access_token_hash, assignee_id, first_response_at, resolved_at, created_at, updated_at |
| comments | id, ticket_id, author_staff_id (null = guest), body, is_internal, created_at |
| attachments | id, ticket_id, file_path, media_type (image/video), size_bytes, uploaded_by_staff_id (null = guest), created_at |
| audit_log | id, actor_type (staff, guest or system), actor_staff_id (set only for staff), action, ticket_id (nullable, no foreign key so entries outlive deleted tickets), target, from_value, to_value, ip_address, created_at |
| locations | id, building, floor, line (each JSONB: en, zh-CN, my, th), is_active; unique (building, floor, line) |
| categories | id, name (JSONB: en, zh-CN, my, th), is_active |

Indexes: tickets(status), tickets(created_at), tickets(location_id), trigram index on tickets(case_details).

## Deployment

Ubuntu Server 24.04 LTS, k3s (installed with `--disable traefik`). Namespaces: `ticket-staging`, `ticket-prod`.

```mermaid
flowchart LR
    U[Browser] -->|HTTPS 443| X[NGINX Ingress]
    X --> A[ticket-app<br/>2 pods]
    A --> P[(PostgreSQL<br/>StatefulSet)]
    A --> R[(Redis)]
    A --> V[Uploads<br/>PersistentVolume]
    A --> G[Gotenberg<br/>PDF]
    H[Vault] -->|secrets via VSO| A
    B[Backup CronJob] --> P
    B --> N[NFS share]
```

| Component | Setup |
| --- | --- |
| Docker image | Multi-stage: Node builds SvelteKit → Go builds static binary → distroless (~20 MB) |
| Registry | Harbor (internal) |
| ticket-app | Deployment, 2 replicas, `/healthz` probes, rolling updates |
| PostgreSQL | StatefulSet + PV; CloudNativePG later if failover needed |
| Migrations | goose Job (image from `deploy/migrate.Dockerfile`) as Argo CD Sync hook in wave 1: after PostgreSQL (wave 0), before the app Deployment (wave 2) |
| Uploads | PV, start 200 GB; NFS (ReadWriteMany) when multi-node |
| NGINX | F5 NGINX Ingress Controller, Helm chart 2.7.3 (controller 5.6.3), values in `deploy/platform/nginx-ingress-values.yaml` (snippets off, `externalTrafficPolicy: Local` so client IPs survive). `deploy/base/ingress.yaml`: a mergeable Ingress; the master holds host, TLS (`ticket-app-tls`) and the `company-network` allow Policy for every path; minions: `/` 101m bodies, `/api/events` no buffering + 1 h read timeout, `/api/track` 30 r/min per client IP, burst 20, 429 past it. The app trusts X-Forwarded-For only from the pod network (`TRUSTED_PROXIES=10.42.0.0/16`). The default access log carries no request headers, so no tracking token |
| Redis | 1 replica, password from Vault, `appendonly yes` on small PV |
| Gotenberg | 1 replica, ClusterIP only, ~512 MB memory; the app sets `GOTENBERG_URL` to its service and `PRINT_BASE_URL` to the app's own ClusterIP service URL, which Gotenberg's Chromium must be able to reach |
| Vault | hashicorp/vault Helm chart 0.34.1 (Vault 2.0.4, values `deploy/platform/vault-values.yaml`), Raft storage, 1 pod (3 for HA), injector off, TLS listener (Secret `vault-tls`, issued by the company CA; VSO trusts it through `vault-ca`); Kubernetes auth; file audit device on its own volume; Shamir unseal 3 of 5. KV v2 at `secret/`, one entry pair per environment: `secret/ticket/<env>/app` (the keys listed in `deploy/base/kustomization.yaml`) and `secret/ticket/<env>/tls` (`tls.crt`, `tls.key`). Policy and role `ticket-<env>`: read-only on that environment, bound to ServiceAccount `ticket-app-vault` in the app namespace, audience `vault`. VSO (chart 1.6.0, `deploy/platform/vso-values.yaml`) syncs them every 60 s into `ticket-app-secrets` and `ticket-app-tls` (`deploy/base/vault.yaml`) and restarts the app when a value changes. Nightly Raft snapshot at 02:30 (CronJob `vault-backup`, `deploy/platform/vault-backup.yaml`, role `vault-backup` that may only read `sys/storage/raft/snapshot`) to the `vault-backups` volume, 14 days kept; restore steps in docs/RESTORE.md |
| TLS | Company CA cert as Ingress Secret, or cert-manager / Vault PKI |
| Backups | CronJob `ticket-backup` (`deploy/base/backup.yaml`), 02:00 nightly: `pg_dump` (custom format, as the owner) then a tar of the uploads, into one dated folder with `SHA256SUMS` on the `ticket-backups` volume (NFS on the host), files 0600; 14 days kept. Both are encrypted with age (image `deploy/backup.Dockerfile`) to the environment's public recipient (`BACKUP_AGE_RECIPIENT` in the app secret); the private identity is only at `secret/backup/<env>` in Vault, outside the app's policy, plus an offline copy, and reaches a restore pod through stdin into a memory-only folder. Redis is not backed up. Restore runbook and monthly restore test: docs/RESTORE.md |
| Manifests | Kustomize, watched by Argo CD (argo/argo-cd Helm chart 10.9.2, Argo CD v3.5.3, values `deploy/platform/argocd-values.yaml`, Git checked every 60 s). Interim: the overlays live in this repo, read through a read-only deploy key (Secret `repo-ticket`, label `argocd.argoproj.io/secret-type: repository`). Two AppProjects (`deploy/argocd/project.yaml`): `ticket-staging` (namespaces `ticket-staging`, `ticket-local`) and `ticket-prod` (namespace `ticket-prod`, with an always-open deny sync window that still allows manual sync). Both allow only this repo, Namespace at cluster scope and the namespaced kinds deploy/base uses (no Secret, Role or RoleBinding). Applications in `deploy/argocd/`: `ticket-staging` syncs `main` by itself (prune, self-heal), `ticket-prod` only by hand. RBAC: everyone signed in reads; only `role:prod-approver` (named local accounts, one per person) may sync `ticket-prod/*`; `admin` is for setting up |
| Firewall (ufw) | 443 open to users; 6443 admins only |

## CI/CD and DevSecOps

GitHub Actions checks every pull request and push (.github/workflows/ci.yml); after CI passes on `main`, .github/workflows/cd.yml builds, scans, signs and pushes the images and commits their digests to the staging overlay. Argo CD deploys by pulling those manifests, so CI never holds cluster credentials.

Interim, until the platform exists (2026-09-28): images go to GHCR instead of Harbor, Cosign signs keyless with the workflow's GitHub OIDC identity instead of a key in Vault, the GitHub token replaces Vault JWT auth, the staging digests live in this repo's `deploy/overlays/staging` instead of a separate GitOps repo, and there is no ZAP stage until staging is up.

```mermaid
flowchart LR
    C[Merge request] --> L[Lint + test]
    L --> S[Security scans]
    S --> B[Build, SBOM,<br/>scan, sign image]
    B --> H[Harbor]
    H --> G[GitOps repo]
    G --> A[Argo CD]
    A --> ST[Staging]
    ST --> Z[ZAP scan]
    Z --> M[Manual approval]
    M --> P[Production]
```

| Stage | Tools | Fails when |
| --- | --- | --- |
| Lint | gofmt, golangci-lint, svelte-check | Any error |
| Unit tests | `go test -race -coverpkg=./...` against PostgreSQL and Redis service containers | Test fails or Go coverage < 70% |
| i18n check | Script comparing message keys | Any language missing a key from en.json |
| End-to-end | Playwright (Edge) against the Go binary serving the built SPA, with Gotenberg; one retry | A test fails twice |
| Secret scan | Gitleaks | Secret in code or history |
| SAST | Semgrep | High-severity finding |
| Dependencies | govulncheck, Trivy fs | Known vuln with fix available |
| IaC | Trivy config, kube-linter | Root, no limits, privileged |
| Build | Docker Buildx | Build error |
| SBOM | Syft (SPDX JSON, attached to the image as a Cosign attestation) | — |
| Image scan | Trivy image, before the push | HIGH/CRITICAL with fix |
| Sign | Cosign (interim: keyless, GitHub OIDC; later key in Vault) | Signing fails |
| DAST | OWASP ZAP baseline on staging | High-risk alert |

Release flow:

- `main` protected; pull request needs passing checks + 1 approval.
- Merge to `main` → staging automatically.
- Tag `vX.Y.Z` → production after manual approval in Argo CD.
- Rollback: revert image tag in GitOps repo.
- CI gets secrets from Vault via GitHub Actions OIDC tokens (JWT auth).
- Kyverno: only Cosign-signed Harbor images, no root, CPU/memory limits required. Four CEL policies (deploy/platform/policies) cover every Pod outside the platform namespaces; the signature is checked against the public half of Vault transit key `cosign`, and each Pod is pinned to the digest that was verified.
- Harbor projects: `ticket` (the app, migration and backup images) and `dockerhub` (signed copies of postgres, redis and gotenberg, not a proxy cache, so nothing unsigned reaches the cluster). Robot accounts: `pull` (nodes and Kyverno) and `ci` (push and sign).
- Harbor nightly rescan; Renovate weekly update pull requests.
