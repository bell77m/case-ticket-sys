#!/usr/bin/env bash
# T3.16 restore test (NFR-8) on the local k3d cluster: take a backup now with the ticket-backup CronJob, restore it
# into the scratch namespace ticket-restore, and prove tickets and evidence came back. docs/RESTORE.md has the steps.
# On the host the scratch pod mounts the NFS backup share read-only; here a static read-only volume on the same
# local-path folder stands in for it, so no backup data passes through kubectl.
set -euo pipefail
export MSYS_NO_PATHCONV=1
K="kubectl --context k3d-ticket-local"
SRC="$K -n ticket-local"
DST="$K -n ticket-restore"
run=$(date +%s)
fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS $1: $2"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }
cleanup() {
  $K delete namespace ticket-restore --ignore-not-found --wait=false >/dev/null
  $K delete pv ticket-restore-backups --ignore-not-found --wait=false >/dev/null
  $SRC delete pod uploads-reader --ignore-not-found --wait=false >/dev/null
  $SRC delete job "backup-test-$run" --ignore-not-found --wait=false >/dev/null
}
trap cleanup EXIT

# Same security settings as the CronJob (deploy/base/backup.yaml); the pods only sleep until the test is done.
pod() { # name namespace uid extra-env-yaml volume-mounts-yaml volumes-yaml
  cat <<EOF
apiVersion: v1
kind: Pod
metadata: { name: $1, namespace: $2 }
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: $3
    runAsGroup: $3
    fsGroup: $3
    seccompProfile: { type: RuntimeDefault }
  containers:
    - name: main
      image: postgres:16-alpine
$4
      resources: { limits: { cpu: "1", memory: 512Mi } }
      securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: [ALL] } }
      volumeMounts:
$5
  volumes:
$6
EOF
}

echo "== backup now (Job from CronJob ticket-backup)"
$SRC create job "backup-test-$run" --from=cronjob/ticket-backup
$SRC wait --for=condition=complete "job/backup-test-$run" --timeout=10m
$SRC logs "job/backup-test-$run"

echo "== scratch namespace: the backup folder read-only, an empty PostgreSQL 16 and an empty uploads folder"
pv=$($SRC get pvc ticket-backups -o jsonpath='{.spec.volumeName}')
$K get pv "$pv" -o json \
  | node -e 'const p=JSON.parse(require("fs").readFileSync(0));process.stdout.write(JSON.stringify({apiVersion:"v1",kind:"PersistentVolume",
      metadata:{name:"ticket-restore-backups"},spec:{capacity:p.spec.capacity,accessModes:["ReadOnlyMany"],
      persistentVolumeReclaimPolicy:"Retain",storageClassName:"",local:p.spec.local,hostPath:p.spec.hostPath,
      nodeAffinity:p.spec.nodeAffinity,claimRef:{namespace:"ticket-restore",name:"backups"}}}))' \
  | $K apply -f -
$K create namespace ticket-restore
$K apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: backups, namespace: ticket-restore }
spec: { accessModes: [ReadOnlyMany], storageClassName: "", volumeName: ticket-restore-backups, resources: { requests: { storage: 1Gi } } }
EOF
# Trust auth, no Service and no network traffic in or out: the database is reached only through kubectl exec and is
# deleted with the namespace. It runs as 65532, the only user that may read the backup files (umask 077).
$K apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: deny-all, namespace: ticket-restore }
spec: { podSelector: {}, policyTypes: [Ingress, Egress] }
EOF
pod restore-db ticket-restore 65532 '      env:
        - { name: POSTGRES_USER, value: ticket }
        - { name: POSTGRES_DB, value: ticket }
        - { name: POSTGRES_HOST_AUTH_METHOD, value: trust }
        - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }' \
  '        - { name: data, mountPath: /var/lib/postgresql/data }
        - { name: run, mountPath: /var/run/postgresql }
        - { name: tmp, mountPath: /tmp }
        - { name: uploads, mountPath: /data/uploads }
        - { name: backups, mountPath: /backups, readOnly: true }' \
  '    - { name: data, emptyDir: {} }
    - { name: run, emptyDir: {} }
    - { name: tmp, emptyDir: {} }
    - { name: uploads, emptyDir: {} }
    - { name: backups, persistentVolumeClaim: { claimName: backups, readOnly: true } }' | $K apply -f -
# The live uploads, read-only, for the checksum comparison.
pod uploads-reader ticket-local 65532 '      command: [sleep, "3600"]' \
  '        - { name: uploads, mountPath: /data/uploads, readOnly: true }' \
  '    - { name: uploads, persistentVolumeClaim: { claimName: ticket-app-uploads, readOnly: true } }' | $K apply -f -
$DST wait --for=condition=ready pod/restore-db --timeout=3m
$SRC wait --for=condition=ready pod/uploads-reader --timeout=3m
until $DST exec restore-db -- pg_isready -U ticket -d ticket -q 2>/dev/null; do sleep 2; done

echo "== restore (docs/RESTORE.md)"
set_dir=/backups/$($DST exec restore-db -- sh -c 'ls -1 /backups | grep -v partial | sort | tail -1')
echo "backup set: $set_dir"
check "checksums" "$($DST exec restore-db -- sh -c "cd $set_dir && sha256sum -c SHA256SUMS >/dev/null && echo ok")" ok
# The StatefulSet's init script creates ticket_app on a new volume; here it is done by hand so the grants restore.
$DST exec restore-db -- psql -v ON_ERROR_STOP=1 -q -U ticket -d ticket -c 'CREATE ROLE ticket_app LOGIN'
$DST exec restore-db -- pg_restore --exit-on-error -U ticket -d ticket "$set_dir/ticket.dump"
$DST exec restore-db -- tar -xf "$set_dir/uploads.tar" -C /data/uploads

echo "== compare with the live system"
q='SELECT (SELECT count(*) FROM tickets)||'"' '"'||(SELECT count(*) FROM attachments)||'"' '"'||(SELECT count(*) FROM comments)||'"' '"'||(SELECT max(id) FROM audit_log)'
src_counts=$($SRC exec postgres-0 -- psql -U ticket -d ticket -tAc "$q")
check "tickets attachments comments last-audit-id" "$($DST exec restore-db -- psql -U ticket -d ticket -tAc "$q")" "$src_counts"
check "attachments in backup" "$(( $(echo "$src_counts" | cut -d' ' -f2) > 0 ))" 1
# Every attachment row has its file, with the recorded size.
missing=$($DST exec restore-db -- sh -c 'psql -U ticket -d ticket -tA -F" " -c "SELECT file_path, size_bytes FROM attachments" |
  while read -r p s; do [ "$(stat -c %s "/data/uploads/$p" 2>/dev/null)" = "$s" ] || echo "$p"; done')
check "attachment files with recorded size" "${missing:-none missing}" "none missing"
sums='cd /data/uploads && find . -type f | sort | xargs -r sha256sum | sha256sum | cut -c1-16'
check "evidence checksums" "$($DST exec restore-db -- sh -c "$sums")" "$($SRC exec uploads-reader -- sh -c "$sums")"
# The append-only rule (FR-L2) survives the restore: ticket_app may insert audit rows but never change them.
check "audit_log grants to ticket_app" "$($DST exec restore-db -- psql -U ticket -d ticket -tAc \
  "SELECT has_table_privilege('ticket_app','audit_log','INSERT')||' '||has_table_privilege('ticket_app','audit_log','UPDATE')||' '||has_table_privilege('ticket_app','audit_log','DELETE')")" "true false false"

[ "$fail" = 0 ] && echo "restore test: all checks passed" || { echo "restore test: FAILED"; exit 1; }
