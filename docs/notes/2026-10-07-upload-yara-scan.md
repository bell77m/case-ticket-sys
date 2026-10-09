# 2026-10-07-upload-yara-scan YARA scan of guest uploads (ClamAV)

Date: 2026-10-07 · Requirements: NFR-5, FR-T1, FR-L1 · Agent: be + fe + ops

## What was done
- Every guest upload that passes the magic-byte and size checks is now scanned with our YARA rules before it is kept. The app streams the stored file to clamd over TCP (INSTREAM, Go standard library only, `backend/internal/api/clamav.go`) before the attachment row is written.
- A match deletes the file, answers 422 `file.malicious` and writes an `attachment.rejected` audit row (guest actor, IP, media type, rule name in to_value). The guest sees "<file> was blocked by the security check" in all 4 languages; staff see it on the ticket timeline and in the activity log.
- If clamd does not answer, or its reply is cut off or unknown, the file is deleted and the guest gets 503 `file.scan_unavailable`: nothing is kept unscanned. `CLAMD_ADDR` (host:port) is a required setting; the app does not start without it.
- Rules (`deploy/base/clamav/ticket-uploads.yar`): programs inside media (Windows PE text, ELF headers, OLE), scripts (`<?php` plus whitespace, `<script`, `<iframe`, `javascript:`, `#!/bin/`, `#!/usr/bin/env`, PowerShell, `base64_decode(`, JSP), and the EICAR test string. Strings are 6 bytes or longer, so random video bytes do not match by chance.
- clamd runs with only these rules (no ClamAV signature database, no freshclam, no internet): in docker-compose for dev and CI, and as Deployment `clamav` in the cluster (non-root 10001, read-only root, caps dropped, ~100 MB). Only the app pods may reach it, and it has no egress (NetworkPolicy `clamav-app-only`).

## Files
- backend/internal/api/clamav.go (new): `scanFile` and the INSTREAM client `clamdScan`; 2-minute deadline; the reply must be NUL-terminated and at most 1 KB.
- backend/internal/api/clamav_test.go (new): fake clamd; TestClamdScan_NFR5 (chunking, clean, match, error reply, cut-off reply, clamd down); TestClamdRules_NFR5 (real clamd, skipped without CLAMD_ADDR).
- backend/internal/api/attachments.go: scan after saving, before the DB transaction; reject and audit on a match.
- backend/internal/api/attachments_test.go: TestUploadMalicious_NFR5, TestUploadScanUnavailable_NFR5.
- backend/internal/api/api.go, backend/cmd/ticket-app/main.go: `Server.ClamdAddr`.
- backend/internal/config/config.go, config_test.go: required `CLAMD_ADDR`, checked as host:port.
- backend/internal/api/helpers_test.go, events_test.go, export_test.go: test servers use the fake clamd.
- deploy/base/clamav/clamd.conf, ticket-uploads.yar (new): clamd config (101 MB limits) and the rules.
- deploy/base/clamav.yaml (new): Deployment and Service.
- deploy/base/kustomization.yaml: resource, Harbor image name, `CLAMD_ADDR=clamav:3310`, ConfigMap `clamav-config` (a changed rule rolls the pod).
- deploy/base/networkpolicy.yaml: `clamav-app-only`.
- deploy/overlays/local/kustomization.yaml: local Harbor image name.
- deploy/install/install.sh: `clamav/clamav:1.5_base` added to THIRD_PARTY (only this line; the file's other uncommitted changes belong to the T3.09 installer work).
- Makefile: `CLAMD_ADDR ?= localhost:3310`; image added to LOCAL_THIRD_PARTY.
- docker-compose.yml: service `clamav` on 127.0.0.1:3310, health check by `zPING`.
- .github/workflows/ci.yml: test and e2e jobs start clamd with `docker compose up -d --wait clamav`; CLAMD_ADDR set.
- frontend/playwright.config.ts: CLAMD_ADDR for the API it starts.
- frontend/src/routes/report/+page.svelte: blocked-file message per file.
- frontend/src/routes/staff/tickets/[id]/+page.svelte, frontend/src/lib/activity.ts: timeline line and activity label for `attachment.rejected`.
- frontend/messages/{en,zh-CN,my,th}.json: `err_file_blocked`, `timeline_attachment_rejected`, `activity_action_attachment_rejected`.
- frontend/e2e/guest-ticket.spec.ts: "photo with a hidden script is blocked".

## Decisions
- ClamAV daemon with YARA rules, rejecting on a match: chosen by the user (2026-10-07) over the yara-x cgo binding and a background sweep.
- Fail closed: an unreachable clamd refuses uploads (503). To relax, skip the scan in `scanFile` when it errors; not recommended.
- No ClamAV signature database: keeps clamd at ~100 MB with no internet access. To add real antivirus signatures, use the non-`_base` image or run freshclam (about 1.2 GB more memory, internet or a mirror).
- The docker-compose health check sends `zPING` over nc: `clamdscan --ping` cannot connect without a `TCPAddr` setting.
- `<?php` must be followed by whitespace, and short markers (`<?=`, appended ZIP) are left out, to avoid false positives on real videos. Files are still served with their exact type, nosniff and a sandbox CSP, so a missed payload cannot run.
- Security review findings accepted: concurrent scans are bounded by clamd (10 threads, CPU/memory limits, 2Gi /tmp) rather than an app semaphore; a crash between saving and scanning can leave an unscanned file on disk with no DB row (never served); a failed `attachment.rejected` audit write is only logged (the upload is still refused).

## Checks
- `make test-go PKG=./internal/api RUN='Upload|Clamd|AuditCoverage'`: TestUploadAttachment_FRT1, TestUploadMalicious_NFR5, TestUploadScanUnavailable_NFR5, TestUploadToClosedTicket_FRT1, TestUploadOutlastsReadTimeout_FRT1, TestAuditCoverage_FRL1, TestClamdScan_NFR5, TestClamdRules_NFR5 (7 samples against real clamd: 2 clean, 5 caught) pass.
- `/check`: `go vet` and `go test -cover ./...` with the dev env: all packages ok; golangci-lint 0 issues; svelte-check 0 errors, 0 warnings; check-i18n passes. The frontend has no `lint` or unit `test` script.
- `npx playwright test e2e/guest-ticket.spec.ts`: 8 passed, including the blocked upload against real clamd.
- `make deploy-lint`: kube-linter no errors and Trivy 0 misconfigurations on staging and prod. actionlint: no findings. `kubectl kustomize` renders staging, prod and local.
- Security review (security-reviewer agent): 7 findings; fixed the cut-off reply and reply size (with a test), the 5-byte `<?php` string, and clamd egress; the image finding was not a defect (base `images:` rewrites it to Harbor); the rest accepted (see Decisions).
- First clamd run caught a rule error: ClamAV's YARA parser rejects `\r` ("illegal escape sequence") and still starts with the other rules. TestClamdRules_NFR5 checks each rule, so a rule that fails to load fails the test.

## Docs updated
- docs/ARCHITECTURE.md: "Upload scan" row, ClamAV in the diagram and the cluster table, clamav in the Harbor dockerhub list.
- docs/INSTALL.md: the installer also copies ClamAV.
- CLAUDE.md: docker compose services include ClamAV :3310.

## Follow-ups
- By hand in .env.example: add `CLAMD_ADDR=localhost:3310` (Claude cannot edit that file). Until then the Makefile, playwright.config.ts and ci.yml set it.
- Local cluster: run `make cluster-deploy` once so the clamav image is in Harbor and signed before Argo CD syncs this change; Kyverno refuses it otherwise.
- docs/REQUIREMENTS.md NFR-5 does not mention the scan; a human may add it (question in PROGRESS.md Blockers).
- Upload route has no per-IP or per-ticket rate limit: one valid tracking token can flood clamd and the disk with parallel uploads (earlier upload review). Fix: one upload at a time per ticket (Redis `SET NX`) and an NGINX limit on `/api/tickets/{id}/attachments`.
- Test new rules against real phone HEIC, MOV and MP4 files before adding them.
