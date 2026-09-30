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

# The CronJob's image (deploy/base/backup.yaml) and the StatefulSet's, so the pods pass the same cluster policies
# (T3.12: Harbor images only, signed).
img=$($SRC get cronjob ticket-backup -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].image}')
dbimg=$($SRC get statefulset postgres -o jsonpath='{.spec.template.spec.containers[0].image}')
secctx='{ allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: [ALL] } }'
podsec='{ runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532, seccompProfile: { type: RuntimeDefault } }'

echo "== backup now (Job from CronJob ticket-backup)"
$SRC create job "backup-test-$run" --from=cronjob/ticket-backup
$SRC wait --for=condition=complete "job/backup-test-$run" --timeout=10m
$SRC logs "job/backup-test-$run"

echo "== scratch namespace: the backup folder read-only, an empty PostgreSQL and an empty uploads folder"
pv=$($SRC get pvc ticket-backups -o jsonpath='{.spec.volumeName}')
$K get pv "$pv" -o json \
  | node -e 'const p=JSON.parse(require("fs").readFileSync(0));process.stdout.write(JSON.stringify({apiVersion:"v1",kind:"PersistentVolume",
      metadata:{name:"ticket-restore-backups"},spec:{capacity:p.spec.capacity,accessModes:["ReadOnlyMany"],
      persistentVolumeReclaimPolicy:"Retain",storageClassName:"",local:p.spec.local,hostPath:p.spec.hostPath,
      nodeAffinity:p.spec.nodeAffinity,claimRef:{namespace:"ticket-restore",name:"backups"}}}))' \
  | $K apply -f -
$K create namespace ticket-restore
# Two containers: "db", an empty PostgreSQL from the StatefulSet's image with trust auth, and "tools", the backup
# image with the backup folder (read-only), an empty uploads folder and a memory-only folder for the private key.
# They run as 65532, the only user that may read the backup files (umask 077). No Service and no traffic in or out:
# reached only through kubectl exec, and deleted with the namespace.
$K apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: deny-all, namespace: ticket-restore }
spec: { podSelector: {}, policyTypes: [Ingress, Egress] }
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: backups, namespace: ticket-restore }
spec: { accessModes: [ReadOnlyMany], storageClassName: "", volumeName: ticket-restore-backups, resources: { requests: { storage: 1Gi } } }
---
apiVersion: v1
kind: Pod
metadata: { name: restore-db, namespace: ticket-restore }
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext: $podsec
  containers:
    - name: db
      image: $dbimg
      env:
        - { name: POSTGRES_USER, value: ticket }
        - { name: POSTGRES_DB, value: ticket }
        - { name: POSTGRES_HOST_AUTH_METHOD, value: trust }
        - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }
      resources: { limits: { cpu: "1", memory: 512Mi } }
      securityContext: $secctx
      volumeMounts:
        - { name: data, mountPath: /var/lib/postgresql/data }
        - { name: run, mountPath: /var/run/postgresql }
        - { name: tmp, mountPath: /tmp }
    - name: tools
      image: $img
      command: [sleep, "3600"]
      env:
        - { name: PGHOST, value: localhost }
        - { name: PGUSER, value: ticket }
        - { name: PGDATABASE, value: ticket }
      resources: { limits: { cpu: "1", memory: 512Mi } }
      securityContext: $secctx
      volumeMounts:
        - { name: uploads, mountPath: /data/uploads }
        - { name: backups, mountPath: /backups, readOnly: true }
        - { name: keys, mountPath: /keys }
  volumes:
    - { name: data, emptyDir: {} }
    - { name: run, emptyDir: {} }
    - { name: tmp, emptyDir: {} }
    - { name: uploads, emptyDir: {} }
    - { name: keys, emptyDir: { medium: Memory, sizeLimit: 1Mi } }
    - { name: backups, persistentVolumeClaim: { claimName: backups, readOnly: true } }
---
# The live uploads, read-only, for the checksum comparison.
apiVersion: v1
kind: Pod
metadata: { name: uploads-reader, namespace: ticket-local }
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext: $podsec
  containers:
    - name: main
      image: $img
      command: [sleep, "3600"]
      resources: { limits: { cpu: "1", memory: 512Mi } }
      securityContext: $secctx
      volumeMounts:
        - { name: uploads, mountPath: /data/uploads, readOnly: true }
  volumes:
    - { name: uploads, persistentVolumeClaim: { claimName: ticket-app-uploads, readOnly: true } }
EOF
$DST wait --for=condition=ready pod/restore-db --timeout=3m
$SRC wait --for=condition=ready pod/uploads-reader --timeout=3m
T="$DST exec restore-db -c tools --"
until $T pg_isready -q 2>/dev/null; do sleep 2; done

echo "== restore (docs/RESTORE.md)"
set_dir=/backups/$($T sh -c 'ls -1 /backups | grep -v partial | sort | tail -1')
echo "backup set: $set_dir"
check "checksums" "$($T sh -c "cd $set_dir && sha256sum -c SHA256SUMS >/dev/null && echo ok")" ok
check "both files age-encrypted" "$($T sh -c "head -c 21 $set_dir/ticket.dump.age; echo; head -c 21 $set_dir/uploads.tar.age")" \
  "$(printf 'age-encryption.org/v1\nage-encryption.org/v1')"
# The private identity: from Vault (local root token, deploy/local/vault-init.json) into the pod's memory folder
# through stdin, as the runbook does with an admin token. It is read into a variable first: on Windows, one
# kubectl exec piped straight into another loses the data. printf is a shell builtin, so it never shows as an argument.
identity=$(node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(0,"utf8")).root_token+"\n")' <deploy/local/vault-init.json |
  $K -n vault exec -i vault-0 -- sh -c 'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault kv get -field=identity secret/backup/local')
printf '%s\n' "$identity" | $DST exec -i restore-db -c tools -- sh -c 'umask 077; cat > /keys/identity'
unset identity
check "a different key cannot decrypt" \
  "$($T sh -c "age-keygen -o /keys/other 2>/dev/null; age -d -i /keys/other $set_dir/ticket.dump.age >/dev/null 2>&1 && echo decrypted || echo refused")" refused
# The StatefulSet's init script creates ticket_app on a new volume; here it is done by hand so the grants restore.
$T psql -v ON_ERROR_STOP=1 -q -c 'CREATE ROLE ticket_app LOGIN'
$T sh -c "set -o pipefail; age -d -i /keys/identity $set_dir/ticket.dump.age | pg_restore --exit-on-error -d ticket"
$T sh -c "set -o pipefail; age -d -i /keys/identity $set_dir/uploads.tar.age | tar -xf - -C /data/uploads"
$T rm -f /keys/identity /keys/other

echo "== compare with the live system"
q='SELECT (SELECT count(*) FROM tickets)||'"' '"'||(SELECT count(*) FROM attachments)||'"' '"'||(SELECT count(*) FROM comments)||'"' '"'||(SELECT max(id) FROM audit_log)'
src_counts=$($SRC exec postgres-0 -- psql -U ticket -d ticket -tAc "$q")
check "tickets attachments comments last-audit-id" "$($T psql -tAc "$q")" "$src_counts"
check "attachments in backup" "$(( $(echo "$src_counts" | cut -d' ' -f2) > 0 ))" 1
# Every attachment row has its file, with the recorded size.
missing=$($T sh -c 'psql -tA -F" " -c "SELECT file_path, size_bytes FROM attachments" |
  while read -r p s; do [ "$(stat -c %s "/data/uploads/$p" 2>/dev/null)" = "$s" ] || echo "$p"; done')
check "attachment files with recorded size" "${missing:-none missing}" "none missing"
sums='cd /data/uploads && find . -type f | sort | xargs -r sha256sum | sha256sum | cut -c1-16'
check "evidence checksums" "$($T sh -c "$sums")" "$($SRC exec uploads-reader -- sh -c "$sums")"
# The append-only rule (FR-L2) survives the restore: ticket_app may insert audit rows but never change them.
check "audit_log grants to ticket_app" "$($T psql -tAc \
  "SELECT has_table_privilege('ticket_app','audit_log','INSERT')||' '||has_table_privilege('ticket_app','audit_log','UPDATE')||' '||has_table_privilege('ticket_app','audit_log','DELETE')")" "true false false"

[ "$fail" = 0 ] && echo "restore test: all checks passed" || { echo "restore test: FAILED"; exit 1; }
