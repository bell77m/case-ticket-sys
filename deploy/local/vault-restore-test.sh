#!/usr/bin/env bash
# Vault backup restore test (T3.16 follow-up) on the local k3d cluster: take a Raft snapshot now with the
# vault-backup CronJob, restore it into a throwaway Vault in the same namespace, unseal it with the real unseal keys,
# and compare it with the live Vault. The live Vault is not touched. docs/RESTORE.md has the steps.
# The scratch Vault listens on 127.0.0.1 inside its pod only (no Service, plain HTTP never leaves the pod). The
# snapshot folder reaches it through a static read-only volume, as a pod on the host would mount the NFS share.
# LOCAL ONLY: it reads the unseal keys and root token from the gitignored deploy/local/vault-init.json.
set -euo pipefail
export MSYS_NO_PATHCONV=1
umask 077
K="kubectl --context k3d-ticket-local -n vault"
init=deploy/local/vault-init.json
run=$(date +%s)
fail=0
check() { if [ "$2" = "$3" ]; then echo "PASS $1: $2"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }
field() { node -e 'const j=JSON.parse(require("fs").readFileSync(0,"utf8"));let x=j;for(const k of process.argv[1].split("."))x=x?.[k];process.stdout.write(x===undefined?"":String(x))' "$1"; }
cleanup() {
  $K delete pod vault-restore-test --ignore-not-found --wait=false >/dev/null
  $K delete configmap vault-restore-test --ignore-not-found >/dev/null
  $K delete pvc vault-backups-view --ignore-not-found --wait=false >/dev/null
  kubectl --context k3d-ticket-local delete pv vault-restore-backups --ignore-not-found --wait=false >/dev/null
  $K delete job "vault-backup-test-$run" --ignore-not-found --wait=false >/dev/null
}
trap cleanup EXIT
# Vault commands with a token as the first stdin line, never an argument: live (vault-0) or scratch.
live() { { printf '%s\n' "$1"; cat; } | $K exec -i vault-0 -- sh -c 'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault "$@"' vault "${@:2}"; }
scratch() { { printf '%s\n' "$1"; cat; } | $K exec -i vault-restore-test -- sh -c 'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault "$@"' vault "${@:2}"; }
root=$(field root_token <"$init")

echo "== snapshot now (Job from CronJob vault-backup)"
$K create job "vault-backup-test-$run" --from=cronjob/vault-backup
$K wait --for=condition=complete "job/vault-backup-test-$run" --timeout=5m
$K logs "job/vault-backup-test-$run" | tail -1

echo "== scratch Vault in namespace vault, with the snapshot folder read-only"
pv=$($K get pvc vault-backups -o jsonpath='{.spec.volumeName}')
kubectl --context k3d-ticket-local get pv "$pv" -o json \
  | node -e 'const p=JSON.parse(require("fs").readFileSync(0));process.stdout.write(JSON.stringify({apiVersion:"v1",kind:"PersistentVolume",
      metadata:{name:"vault-restore-backups"},spec:{capacity:p.spec.capacity,accessModes:["ReadOnlyMany"],
      persistentVolumeReclaimPolicy:"Retain",storageClassName:"",local:p.spec.local,hostPath:p.spec.hostPath,
      nodeAffinity:p.spec.nodeAffinity,claimRef:{namespace:"vault",name:"vault-backups-view"}}}))' \
  | kubectl --context k3d-ticket-local apply -f - >/dev/null
$K apply -f - >/dev/null <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: vault-backups-view, namespace: vault }
spec: { accessModes: [ReadOnlyMany], storageClassName: "", volumeName: vault-restore-backups, resources: { requests: { storage: 1Gi } } }
---
apiVersion: v1
kind: ConfigMap
metadata: { name: vault-restore-test, namespace: vault }
data:
  config.hcl: |
    storage "raft" {
      path    = "/vault/data"
      node_id = "restore-test"
    }
    listener "tcp" {
      address     = "127.0.0.1:8200"
      tls_disable = true
    }
    api_addr      = "http://127.0.0.1:8200"
    cluster_addr  = "http://127.0.0.1:8201"
    disable_mlock = true
---
apiVersion: v1
kind: Pod
metadata: { name: vault-restore-test, namespace: vault }
spec:
  restartPolicy: Always # the restore needs a Vault restart; emptyDir data survives it
  automountServiceAccountToken: false
  securityContext: { runAsNonRoot: true, runAsUser: 100, runAsGroup: 1000, fsGroup: 1000, seccompProfile: { type: RuntimeDefault } }
  containers:
    - name: vault
      image: hashicorp/vault:2.0.4
      command: [vault, server, -config=/config/config.hcl]
      env: [{ name: VAULT_ADDR, value: "http://127.0.0.1:8200" }, { name: HOME, value: /tmp }]
      resources: { limits: { cpu: 500m, memory: 256Mi } }
      securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: [ALL] } }
      volumeMounts:
        - { name: config, mountPath: /config, readOnly: true }
        - { name: data, mountPath: /vault/data }
        - { name: audit, mountPath: /vault/audit } # the restored file audit device writes here, or Vault stays standby
        - { name: tmp, mountPath: /tmp }
        - { name: backups, mountPath: /backups, readOnly: true }
  volumes:
    - { name: config, configMap: { name: vault-restore-test } }
    - { name: data, emptyDir: {} }
    - { name: audit, emptyDir: {} }
    - { name: tmp, emptyDir: {} }
    - { name: backups, persistentVolumeClaim: { claimName: vault-backups-view, readOnly: true } }
EOF
$K wait --for=jsonpath='{.status.phase}'=Running pod/vault-restore-test --timeout=3m >/dev/null
until $K exec vault-restore-test -- vault status >/dev/null 2>&1 || [ $? = 2 ]; do sleep 2; done

echo "== restore (docs/RESTORE.md, Vault)"
snap=/backups/$($K exec vault-restore-test -- sh -c 'ls -1 /backups | grep -E "^vault-.*\.snap$" | sort | tail -1')
echo "snapshot: $snap"
# A fresh Vault needs its own init and unseal before it accepts the snapshot; -force replaces everything with it.
scratch_init=$($K exec vault-restore-test -- vault operator init -key-shares=1 -key-threshold=1 -format=json)
printf '{"key":"%s"}' "$(echo "$scratch_init" | field unseal_keys_b64.0)" | $K exec -i vault-restore-test -- vault write -format=json sys/unseal - >/dev/null
until scratch "$(echo "$scratch_init" | field root_token)" operator raft list-peers </dev/null >/dev/null 2>&1; do sleep 2; done
scratch "$(echo "$scratch_init" | field root_token)" operator raft snapshot restore -force "$snap" </dev/null
unset scratch_init
# The restored data carries the live seal settings and keyring, which Vault reads at start: restart it, and it then
# opens only with the live unseal keys (docs/RESTORE.md).
$K exec vault-restore-test -- kill 1 || true
until [ "$($K get pod vault-restore-test -o jsonpath='{.status.containerStatuses[0].restartCount}')" -ge 1 ] &&
  { $K exec vault-restore-test -- vault status >/dev/null 2>&1; [ $? = 2 ]; }; do sleep 2; done
check "sealed after restart" "$($K exec vault-restore-test -- vault status -format=json 2>/dev/null | field sealed)" true
for i in 0 1 2; do
  printf '{"key":"%s"}' "$(field "unseal_keys_b64.$i" <"$init")" | $K exec -i vault-restore-test -- vault write -format=json sys/unseal - >/dev/null
done
check "unsealed with the live keys" "$($K exec vault-restore-test -- vault status -format=json 2>/dev/null | field sealed)" false
t0=$(date +%s)
until scratch "$root" kv list -format=json secret/ </dev/null >/dev/null 2>&1; do
  if [ $(( $(date +%s) - t0 )) -gt 120 ]; then
    echo "restored Vault does not answer after 120 s:"; $K exec vault-restore-test -- vault status || true
    scratch "$root" kv list secret/ </dev/null || true; $K logs vault-restore-test --tail=20 | cut -c1-220; exit 1
  fi
  sleep 2
done

echo "== compare with the live Vault"
# Keys by name, values only as a hash of the whole data (never printed).
keys='const d=JSON.parse(require("fs").readFileSync(0,"utf8")).data.data;process.stdout.write(Object.keys(d).sort().join(","))'
hash='const d=JSON.parse(require("fs").readFileSync(0,"utf8")).data.data;process.stdout.write(require("crypto").createHash("sha256").update(JSON.stringify(Object.entries(d).sort())).digest("hex").slice(0,16))'
check "backup recipient" "$(scratch "$root" kv get -field=recipient secret/backup/local </dev/null)" \
  "$(live "$root" kv get -field=recipient secret/backup/local </dev/null)"
for p in ticket/local/app ticket/local/tls backup/local; do
  check "$p keys" "$(scratch "$root" kv get -format=json "secret/$p" </dev/null | node -e "$keys")" \
    "$(live "$root" kv get -format=json "secret/$p" </dev/null | node -e "$keys")"
  check "$p values (hash)" "$(scratch "$root" kv get -format=json "secret/$p" </dev/null | node -e "$hash")" \
    "$(live "$root" kv get -format=json "secret/$p" </dev/null | node -e "$hash")"
done
check "policies" "$(scratch "$root" policy list </dev/null | sort | tr '\n' ' ')" "$(live "$root" policy list </dev/null | sort | tr '\n' ' ')"
check "kubernetes auth roles" "$(scratch "$root" list -format=json auth/kubernetes/role </dev/null | tr -d ' \n\r')" \
  "$(live "$root" list -format=json auth/kubernetes/role </dev/null | tr -d ' \n\r')"

[ "$fail" = 0 ] && echo "vault restore test: all checks passed" || { echo "vault restore test: FAILED"; exit 1; }
