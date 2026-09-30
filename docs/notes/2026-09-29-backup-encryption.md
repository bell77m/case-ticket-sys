# 2026-09-29-backup-encryption Encrypt the nightly backups with age

Date: 2026-09-29 · Requirements: NFR-8 (and NFR-7 for the key) · Agent: ops (main agent)

## What was done
- The nightly backup (T3.16) now writes `ticket.dump.age` and `uploads.tar.age`. `pg_dump` and `tar` pipe straight into `age -r <recipient>`, so nothing unencrypted touches the backup share. `set -o pipefail` makes a failed dump fail the run.
- Each environment has one age key pair:
  - the public recipient travels with the app secrets (`BACKUP_AGE_RECIPIENT`, delivered by VSO);
  - the private identity is stored only at `secret/backup/<env>` in Vault, outside the app's policy path, plus an offline copy.
  The cluster never holds the identity except during a restore.
- A restore gives the identity to the restore pod through stdin, into a memory-only folder that goes away with the pod. The restore test and the runbook (docs/RESTORE.md) do this, including the new "The backup key" section.
- New image `ticket-backup` (`deploy/backup.Dockerfile`: alpine 3.24, postgresql16-client 16.15, age 1.3.1, user 65532). It runs the CronJob and the restore pods. CD builds, scans, pushes, signs and attests it with the other two images and puts its digest in the staging overlay.

## Files
- deploy/backup.Dockerfile (new): the backup image.
- deploy/base/backup.yaml: age in the pipe, `BACKUP_AGE_RECIPIENT`, pipefail, the new image.
- deploy/base/kustomization.yaml: `BACKUP_AGE_RECIPIENT` listed among the secret keys.
- deploy/overlays/{local,staging,prod}/kustomization.yaml: the `ticket-backup` image. Staging has a placeholder tag until the first CD run.
- .github/workflows/cd.yml: the third image in build, Trivy, push, SBOM, sign, attest and the digest bump.
- deploy/local/vault-setup.sh: makes the key once and adds the recipient to the app secret.
- deploy/restore/restore-pod.yaml: backup image, memory-only `/keys`.
- deploy/local/restore-test.sh: DB and tools as two containers; the identity from Vault into `/keys`; new checks (age header, a different key refused).
- Makefile: `cluster-images` builds and imports `ticket-backup:local`; `cluster-deploy` builds the images before `cluster-vault`.
- docs/RESTORE.md, docs/ARCHITECTURE.md: see Docs updated.

## Decisions
- The user chose encryption with age on 2026-09-29. age streams and authenticates, and is one small package. openssl CMS was not used: it would need the whole file in memory for large video sets.
- The identity is not synced by VSO into any Kubernetes Secret, so a stolen cluster credential cannot read backups. That departs slightly from "private key in Vault (VSO)" in the option as offered: Vault yes, VSO no.
- The key is made once and never replaced by the script:
  - only a real "No value found" creates one; any other Vault error stops the script;
  - the write uses KV check-and-set `cas=0`.
  Rotation is manual (runbook): keep the old identity until its last set has aged out (14 days).
- Locally the identity is not printed; on the host the runbook has an admin print it once for the offline copy.

## Checks
- `make cluster-restore-test` passes all checks on an encrypted set:
  - checksums ok, and both files start with `age-encryption.org/v1`;
  - a different key is refused;
  - counts `7 6 0 193` equal the live database;
  - no attachment missing, evidence checksum `e00ed782766f48c8`;
  - audit_log grants: `true false false`.
- The CronJob log shows the set, `-rw-------` 65532.
- Trivy image scan of `ticket-backup:local` (the CD gate): 0 HIGH, 0 CRITICAL.
- `make deploy-lint`: kube-linter "No lint errors found!" for staging and prod; Trivy config clean.
- actionlint on ci.yml and cd.yml: clean. SC2129 was fixed by grouping the `GITHUB_ENV` lines.
- Security review (backup.yaml, Dockerfile, vault-setup.sh block, restore pod and test): 1 low, fixed (the key-minting guard described above). Otherwise it confirmed:
  - the identity never reaches a Secret, an argument, a log or the app's Vault role;
  - a failed dump leaves only a `.partial` folder.
- First try: piping one `kubectl exec` straight into another on Windows lost the identity ("The pipe is being closed"). The test now reads it into a shell variable and sends it with the `printf` builtin.

## Docs updated
- docs/RESTORE.md: file names, the new "The backup key" section, the restore-test checks, and the restore steps with the identity and `age -d`.
- docs/ARCHITECTURE.md: the Backups row describes the encryption and where each half of the key lives.

## Follow-ups
- T3.09 host:
  - make the prod and staging keys as in the runbook;
  - print the identity once for the offline copy, kept with the unseal keys;
  - give the Vault admin policy read access to `secret/backup/*`.
- T3.12: the policies require Harbor images, so `ticket-backup` must go through Harbor and be signed like the other images (said to session d6).
