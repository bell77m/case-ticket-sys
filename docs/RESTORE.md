# Backups and restore

For the ops person on call. Covers NFR-8 (daily backups of the database and uploads, with a tested restore); built in T3.16.

## What is backed up

The CronJob `ticket-backup` (`deploy/base/backup.yaml`) runs every night at 02:00 in each app namespace (`ticket-staging`, `ticket-prod`). It writes one folder per run on the backup volume `ticket-backups` (the NFS share):

```
/backups/2026-09-29T020000Z/
  ticket.dump.age   pg_dump of database "ticket", custom format, encrypted with age
  uploads.tar.age   tar of every evidence photo and video (paths relative to UPLOAD_DIR), encrypted with age
  SHA256SUMS        checksums of both files
```

- Both files are encrypted with [age](https://age-encryption.org) to the environment's backup key (see "The backup key" below), and readable only by user 65532 (the app's user). Still export the NFS share only to the cluster node's IP, with `root_squash`.
- A run that fails leaves a folder ending in `.partial`. Never restore from one.
- Folders older than 14 days are deleted (`BACKUP_KEEP_DAYS` in the CronJob).
- Worst case, a restore loses the last 24 hours of tickets.

Not backed up, on purpose:
- **Redis.** It holds only sessions, rate-limit counters and live-update messages. After a restore, staff sign in again.
- **Kubernetes objects.** They are in Git (`deploy/`), and Argo CD recreates them.
- **Vault** is backed up separately, by its own snapshot (see "Vault" at the end).

## The backup key

One age key pair per environment. The cluster only ever holds the public half.
- **Public recipient** (`age1…`): the key `BACKUP_AGE_RECIPIENT` in `secret/ticket/<env>/app`, which VSO delivers to the CronJob with the other app secrets.
- **Private identity** (`AGE-SECRET-KEY-1…`): `secret/backup/<env>` in Vault (fields `identity` and `recipient`). This is outside the app's policy path, so only an admin token can read it. There is also one offline copy (see below). Without the identity, no backup can be read.

Make it once per environment, on an admin machine with Docker and a Vault admin login:

```sh
docker run --rm ticket-backup:<tag> age-keygen            # prints the identity; its public key is in the comment line
vault kv put -cas=0 secret/backup/prod identity=- recipient=age1…   # paste the identity on stdin; -cas=0 never overwrites
vault kv patch secret/ticket/prod/app BACKUP_AGE_RECIPIENT=age1…
```

Write the identity on paper or in the password manager's offline vault, and keep it in the safe with the Vault unseal keys, then clear the terminal. Locally, `make cluster-vault` does all of this (deploy/local/vault-setup.sh), without printing the identity.

A new key does not re-encrypt old sets: keep the old identity until its last set is older than 14 days.

Check that last night's backup ran:

```sh
kubectl -n ticket-prod get cronjob ticket-backup          # LAST SCHEDULE within 24 h
kubectl -n ticket-prod get jobs -l app.kubernetes.io/name=ticket-backup
kubectl -n ticket-prod logs job/<newest job>              # lists the three files and their sizes
```

## Restore test (monthly, and after any change to the backup)

Restore the newest backup into a scratch namespace, then compare it with the live system. The live system is not touched.

- On the local cluster, `make cluster-restore-test` does all of it (`deploy/local/restore-test.sh`).
- On the host, do the same steps, with the scratch pod mounting the NFS share read-only.

The test passes when:
- the checksums match;
- both files start with the age header, and a different key cannot decrypt them;
- the counts of tickets, attachments and comments, and the last audit_log id, equal the live database at backup time;
- every attachment row has its file, with the recorded size;
- the evidence files have the same SHA-256 as the live ones;
- `ticket_app` may insert into `audit_log` but not update or delete (FR-L2).

## Full restore in place (data lost or corrupted)

Set the namespace first: `NS=ticket-prod` (or `ticket-staging`). Commands run from the repo root.

1. **Stop Argo CD from undoing the steps below.**
   - Staging syncs by itself, so turn that off: `kubectl -n argocd patch application ticket-staging --type merge -p '{"spec":{"syncPolicy":{"automated":null}}}'`.
   - Prod syncs only by hand, so do not press Sync during the restore.

2. **Stop writes.** Suspend the backup so it does not run halfway through, then stop the app:
   ```sh
   kubectl -n $NS patch cronjob ticket-backup -p '{"spec":{"suspend":true}}'
   kubectl -n $NS scale deploy/ticket-app --replicas=0
   ```

3. **PostgreSQL must be running.**
   - If its volume was lost, the StatefulSet creates an empty one. On first start, the init script creates `ticket_app` with the password from Vault.
   - Wait until it is ready: `kubectl -n $NS rollout status statefulset/postgres`.

4. **Start the restore pod** with the image the CronJob uses:
   ```sh
   IMG=$(kubectl -n $NS get cronjob ticket-backup -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].image}')
   sed "s|image: ticket-backup|image: $IMG|" deploy/restore/restore-pod.yaml | kubectl -n $NS apply -f -
   kubectl -n $NS wait --for=condition=ready pod/ticket-restore
   ```

5. **Pick the backup and check it.** Take the newest folder from before the damage.
   ```sh
   kubectl -n $NS exec ticket-restore -- ls -1 /backups
   SET=/backups/<folder>
   kubectl -n $NS exec ticket-restore -- sh -c "cd $SET && sha256sum -c SHA256SUMS"
   ```
   Then give the pod the private identity. It goes into a memory-only folder and is gone with the pod. It never goes into a Secret or onto a command line:
   ```sh
   IDENTITY=$(vault kv get -field=identity secret/backup/prod)     # your admin login; staging: secret/backup/staging
   printf '%s
' "$IDENTITY" | kubectl -n $NS exec -i ticket-restore -- sh -c 'umask 077; cat > /keys/identity'
   unset IDENTITY
   ```

6. **Restore the database.** This runs as the owner `ticket`. `--clean` drops each object before it is recreated, so the database ends up exactly as in the backup, audit_log included.
   ```sh
   kubectl -n $NS exec ticket-restore -- sh -c "set -o pipefail; age -d -i /keys/identity $SET/ticket.dump.age | pg_restore --clean --if-exists --exit-on-error -d ticket"
   ```

7. **Restore the evidence files.** Files in the backup are written back. A file uploaded after the backup stays on disk, but no ticket points to it.
   ```sh
   kubectl -n $NS exec ticket-restore -- sh -c "set -o pipefail; age -d -i /keys/identity $SET/uploads.tar.age | tar -xf - -C /data/uploads"
   ```

8. **Start again and clean up.**
   ```sh
   kubectl -n $NS delete pod ticket-restore            # also removes the identity (memory-only folder)
   kubectl -n $NS scale deploy/ticket-app --replicas=2
   kubectl -n $NS rollout status deploy/ticket-app
   kubectl -n $NS patch cronjob ticket-backup -p '{"spec":{"suspend":false}}'
   kubectl apply -f deploy/argocd/staging.yaml     # staging only: turns automatic sync back on
   ```
   If the app image is newer than the backup, the migration Job brings the schema up to date at the next sync.

9. **Check.**
   - `/healthz` answers 200.
   - A staff member signs in and opens a ticket with a photo, and the photo loads.
   - The newest ticket number is from just before the backup time.
   - Tell staff that changes made after the backup time are gone: tickets, replies and status changes.
   - Record the restore in the ops change log: time, backup folder and who ran it. `audit_log` itself went back to the backup's state, so it cannot record the restore.

## Vault

The CronJob `vault-backup` (`deploy/platform/vault-backup.yaml`, namespace `vault`) takes a Raft snapshot of all of Vault every night at 02:30. That covers every secret, the policies, the auth roles, the backup key and the Cosign key.
- It writes `vault-<time>.snap` (0600) to the volume `vault-backups`, which on the host is the NFS share, and keeps 14 days.
- It signs in with its own Kubernetes auth role `vault-backup`, whose policy may only read `sys/storage/raft/snapshot`, and revokes its token at the end.
- A snapshot is still encrypted by Vault's barrier: it opens only with 3 of the 5 unseal keys. Without the unseal keys it is useless, and so are the app backups (their key is inside it).

Restore test (monthly, with the app's): `make cluster-vault-restore-test` locally (`deploy/local/vault-restore-test.sh`). The live Vault is not touched. The test:
- takes a snapshot now;
- restores it into a throwaway Vault in namespace `vault`;
- unseals that Vault with the live keys;
- compares the secrets (as hashes), the policies and the auth roles with the live Vault.

Full restore (Vault's data lost):
1. Install Vault as usual (`deploy/platform/vault-values.yaml`, with its audit volume: the restored settings turn the file audit device back on, and Vault stays standby if it cannot write `/vault/audit/audit.log`). Initialise it with 1 key share, unseal it with that key, and keep its temporary root token.
2. Copy the newest snapshot into the pod, or mount the share, then:
   `vault operator raft snapshot restore -force /path/vault-<time>.snap` (with the temporary root token on stdin as `VAULT_TOKEN`, never as an argument).
3. Restart Vault (`kubectl -n vault delete pod vault-0`). It comes back sealed with the old settings: unseal it with 3 of the original 5 keys. The temporary key and root token are gone with the old data.
4. VSO reconnects by itself; check that `ticket-app-secrets` and `ticket-app-tls` are synced (`kubectl get vaultstaticsecret -A`).
