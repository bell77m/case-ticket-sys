# 2026-09-29-vault-backup Nightly Vault snapshot with a tested restore

Date: 2026-09-29 · Requirements: NFR-8, NFR-7 (T3.16 follow-up) · Agent: ops (main agent)

## What was done
- The CronJob `vault-backup` (namespace `vault`, 02:30 nightly) saves a Raft snapshot of all of Vault to the `vault-backups` volume (0600, 14 days kept). The snapshot holds every secret, policy and auth role, the backup key and the Cosign key.
- It signs in with its own Kubernetes auth role `vault-backup`:
  - projected ServiceAccount token, audience `vault`, 10 minutes;
  - policy: read `sys/storage/raft/snapshot` and nothing else;
  - its Vault token is revoked at the end;
  - only the CA certificate of `vault-tls` is mounted.
- `make cluster-vault-restore-test` proves a restore without touching the live Vault:
  - takes a snapshot now;
  - restores it into a throwaway Vault in the same namespace;
  - restarts it, then unseals it with the live keys;
  - compares the secrets (as hashes), the policies and the auth roles.
- docs/RESTORE.md has a new "Vault" section: what the snapshot holds, the restore test and a full restore.

## Files
- deploy/platform/vault-backup.yaml (new): ServiceAccount, CronJob and PVC.
- deploy/local/vault-setup.sh: the `vault-backup` policy and role.
- deploy/local/vault-restore-test.sh (new): the restore test.
- Makefile: `cluster-vault-backup` (runs cluster-vault, then applies the CronJob) and `cluster-vault-restore-test`.
- docs/RESTORE.md, docs/ARCHITECTURE.md, CLAUDE.md: see Docs updated.

## Decisions
- The snapshot is not encrypted again: Vault's barrier already encrypts it, so it opens only with 3 of the 5 unseal keys. It should still sit on the NFS share, locked to the node, like the app backups.
- The CronJob is a platform piece in namespace `vault`, applied with the platform, not by Argo CD: the AppProjects only reach the app namespaces.
- The scratch Vault listens on 127.0.0.1 inside its pod with plain HTTP, so nothing outside the pod can reach it. It runs in namespace `vault`, which the T3.12 policies leave to the platform.

## Checks
- Test first: `vault-restore-test.sh` before the CronJob existed failed with `cronjobs.batch "vault-backup" not found`.
- The snapshot Job wrote `vault-2026-09-30T023615Z.snap`, 78555 bytes, `-rw-------` owned by vault.
- `make cluster-vault-restore-test`: 13/13 PASS.
  - Sealed after the restart; unsealed with 3 of the live keys.
  - Backup recipient equal to the live Vault.
  - Keys and value hashes equal for `ticket/local/app`, `ticket/local/tls` and `backup/local`.
  - Policies equal: `cosign-sign default default-ceiling root ticket-local vault-backup`.
  - Kubernetes auth roles equal: `ticket-local`, `vault-backup`.
- Afterwards the scratch pod, ConfigMap, PVC, static PV and test Jobs are gone.
- Two runs failed first, and the fixes are now in the runbook:
  1. After `snapshot restore -force`, the new Vault still used its own 1-share seal settings and refused the live keys (`invalid key size 33`). It has to be restarted to load the restored ones.
  2. The restored settings turn the file audit device back on. With no writable `/vault/audit`, Vault stayed standby ("failed to setup audit table"). Any restore target needs the audit volume.
- Security review: see the PROGRESS log line.

## Docs updated
- docs/RESTORE.md: the "Not backed up" line for Vault now points to the new "Vault" section.
- docs/ARCHITECTURE.md: the Vault row names the snapshot CronJob and its role.
- CLAUDE.md: the local cluster line has the two new targets.

## Follow-ups
- T3.09 host:
  - point `vault-backups` at the NFS share;
  - run the policy and role commands with the admin token;
  - run the restore test once there, then monthly with the app's.
- Alerting on a failed backup Job, with monitoring (as for the app backup).
