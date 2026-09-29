# Backups and restore

For the ops person on call. Covers NFR-8 (daily backups of the database and uploads, with a tested restore); built in T3.16.

## What is backed up

The CronJob `ticket-backup` (`deploy/base/backup.yaml`) runs every night at 02:00 in each app namespace (`ticket-staging`, `ticket-prod`). It writes one folder per run on the backup volume `ticket-backups` (the NFS share):

```
/backups/2026-09-29T020000Z/
  ticket.dump     pg_dump of database "ticket", custom format (pg_restore reads it)
  uploads.tar     every evidence photo and video, paths relative to UPLOAD_DIR
  SHA256SUMS      checksums of both files
```

- The files are readable only by user 65532 (the app's user). They are not encrypted, so the NFS share must be exported only to the cluster node's IP, with `root_squash`, and no other host may mount it.
- A run that fails leaves a folder ending in `.partial`. Never restore from one.
- Folders older than 14 days are deleted (`BACKUP_KEEP_DAYS` in the CronJob).
- Worst case, a restore loses the last 24 hours of tickets.

Not backed up, on purpose:
- **Redis.** It holds only sessions, rate-limit counters and live-update messages. After a restore, staff sign in again.
- **Kubernetes objects.** They are in Git (`deploy/`), and Argo CD recreates them.
- **Vault.** Vault has its own snapshot procedure; see Follow-ups in docs/notes/T3.16.md.

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
   sed "s|image: postgres:16-alpine|image: $IMG|" deploy/restore/restore-pod.yaml | kubectl -n $NS apply -f -
   kubectl -n $NS wait --for=condition=ready pod/ticket-restore
   ```

5. **Pick the backup and check it.** Take the newest folder from before the damage.
   ```sh
   kubectl -n $NS exec ticket-restore -- ls -1 /backups
   SET=/backups/<folder>
   kubectl -n $NS exec ticket-restore -- sh -c "cd $SET && sha256sum -c SHA256SUMS"
   ```

6. **Restore the database.** This runs as the owner `ticket`. `--clean` drops each object before it is recreated, so the database ends up exactly as in the backup, audit_log included.
   ```sh
   kubectl -n $NS exec ticket-restore -- pg_restore --clean --if-exists --exit-on-error -d ticket $SET/ticket.dump
   ```

7. **Restore the evidence files.** Files in the backup are written back. A file uploaded after the backup stays on disk, but no ticket points to it.
   ```sh
   kubectl -n $NS exec ticket-restore -- tar -xf $SET/uploads.tar -C /data/uploads
   ```

8. **Start again and clean up.**
   ```sh
   kubectl -n $NS delete pod ticket-restore
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
