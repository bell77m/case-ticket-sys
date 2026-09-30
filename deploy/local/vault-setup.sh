#!/usr/bin/env bash
# T3.11 on the local k3d cluster (make cluster-vault): initialise and unseal Vault (5 key shares, 3 needed), turn on
# the file audit device, enable KV v2 and Kubernetes auth, write the ticket-local read-only policy and role, then load
# the local secrets from the gitignored deploy/overlays/local/secrets.env and tls/. Safe to run again: every step
# checks first, so it also unseals Vault after a restart.
#
# LOCAL ONLY: the unseal keys and the root token land in the gitignored deploy/local/vault-init.json. On the T3.09
# host the five keys go to five people and the root token is revoked after setup (docs/notes/T3.11.md). Keys and
# tokens travel on stdin, never as command arguments (they would show in process lists and the API server's audit log).
set -euo pipefail
export MSYS_NO_PATHCONV=1 # Git Bash: pass paths such as /vault/audit/audit.log into the pod as written
umask 077

ctx=k3d-ticket-local
env=local
ns=ticket-local
init=deploy/local/vault-init.json
secrets=deploy/overlays/local/secrets.env
tls=deploy/overlays/local/tls

k() { kubectl --context "$ctx" "$@"; }
# vault without a token (status, init, unseal); stdin passes through.
v0() { k -n vault exec -i vault-0 -- vault "$@"; }
# vault with the token as the first line of stdin, then the command's own input (if any) after it.
v() { { printf '%s\n' "$VAULT_TOKEN"; [ -t 0 ] || cat; } | k -n vault exec -i vault-0 -- sh -c 'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault "$@"' vault "$@"; }
field() { node -e 'const j=JSON.parse(require("fs").readFileSync(0,"utf8"));let x=j;for(const k of process.argv[1].split("."))x=x[k];process.stdout.write(String(x))' "$1"; }

# Vault's pod runs but is not Ready while sealed, so wait for Running, not Ready.
k -n vault wait --for=jsonpath='{.status.phase}'=Running pod/vault-0 --timeout=5m >/dev/null

# vault status: exit 0 = unsealed, 2 = sealed, anything else = could not ask. Never guess on an error.
rc=0
status=$(v0 status -format=json </dev/null) || rc=$?
[ "$rc" = 0 ] || [ "$rc" = 2 ] || { echo "vault status failed (exit $rc); not touching Vault" >&2; exit 1; }
if [ "$(echo "$status" | field initialized)" != true ]; then
	[ ! -e "$init" ] || { echo "$init exists but Vault is not initialised; refusing to overwrite the keys" >&2; exit 1; }
	echo "initialising Vault (5 key shares, threshold 3)"
	v0 operator init -key-shares=5 -key-threshold=3 -format=json </dev/null >"$init.tmp"
	mv "$init.tmp" "$init"
	status=$(v0 status -format=json </dev/null) || true
fi
if [ "$(echo "$status" | field sealed)" = true ]; then
	echo "unsealing Vault with 3 of 5 keys"
	for i in 0 1 2; do
		printf '{"key":"%s"}' "$(field "unseal_keys_b64.$i" <"$init")" | v0 write -format=json sys/unseal - >/dev/null
	done
fi
VAULT_TOKEN=$(field root_token <"$init")

# Audit first, so everything after it is recorded (hashed) from the very first run.
v audit list -format=json </dev/null 2>/dev/null | grep -q '"file/"' ||
	v audit enable file file_path=/vault/audit/audit.log </dev/null
v secrets list -format=json </dev/null | grep -q '"secret/"' || v secrets enable -path=secret kv-v2 </dev/null
v auth list -format=json </dev/null | grep -q '"kubernetes/"' || v auth enable kubernetes </dev/null
# In the cluster, Vault checks service-account tokens with its own ServiceAccount (the chart's auth-delegator binding).
v write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc </dev/null >/dev/null

# Read-only access to this environment's entries, for the ServiceAccount VSO uses in this namespace (vault.yaml).
printf 'path "secret/data/ticket/%s/*" {\n  capabilities = ["read"]\n}\n' "$env" | v policy write "ticket-$env" - >/dev/null
v write "auth/kubernetes/role/ticket-$env" bound_service_account_names=ticket-app-vault \
	bound_service_account_namespaces="$ns" policies="ticket-$env" audience=vault ttl=1h </dev/null >/dev/null

# Vault's own backup (deploy/platform/vault-backup.yaml): its ServiceAccount may take a Raft snapshot, nothing else.
printf 'path "sys/storage/raft/snapshot" {\n  capabilities = ["read"]\n}\n' | v policy write vault-backup - >/dev/null
v write auth/kubernetes/role/vault-backup bound_service_account_names=vault-backup \
	bound_service_account_namespaces=vault policies=vault-backup audience=vault ttl=15m </dev/null >/dev/null

# T3.16: backups are encrypted with age. The private identity is kept only at secret/backup/<env>, outside the
# path the app's policy may read (a restore reads it with an admin token); its public recipient goes into the app
# secret for the backup CronJob. Made once with age-keygen from the backup image (make cluster-images builds it).
# Only a real "not there yet" makes a key: any other failure stops here, because a new key would leave every backup
# made with the old one unreadable. The write below also refuses to replace an existing entry (cas=0).
rc=0
current=$(v kv get -format=json "secret/backup/$env" </dev/null 2>&1) || rc=$?
if [ "$rc" = 0 ]; then
	recipient=$(echo "$current" | field data.data.recipient)
	case "$recipient" in age1*) ;; *) echo "secret/backup/$env has no age recipient; fix it by hand" >&2; exit 1 ;; esac
elif printf '%s' "$current" | grep -q 'No value found at'; then
	recipient=
else
	echo "reading secret/backup/$env failed (exit $rc); not making a new backup key" >&2
	exit 1
fi
if [ -z "$recipient" ]; then
	echo "creating the backup key (age) at secret/backup/$env"
	identity=$(docker run --rm ticket-backup:local age-keygen 2>/dev/null | grep '^AGE-SECRET-KEY-') ||
		{ echo "no ticket-backup:local image to make the backup key: run make cluster-images first" >&2; exit 1; }
	recipient=$(printf '%s\n' "$identity" | docker run --rm -i ticket-backup:local age-keygen -y)
	case "$recipient" in age1*) ;; *) echo "age-keygen -y gave no recipient" >&2; exit 1 ;; esac
	printf '{"options":{"cas":0},"data":{"identity":"%s","recipient":"%s"}}' "$identity" "$recipient" |
		v write "secret/data/backup/$env" - >/dev/null
	unset identity
fi

# The values: app secrets from secrets.env plus the backup recipient, the certificate from tls/ (written as KV v2
# JSON bodies on stdin).
BACKUP_AGE_RECIPIENT="$recipient" node -e 'const l=require("fs").readFileSync(process.argv[1],"utf8").split(/\r?\n/).filter(Boolean);const d={};for(const x of l){const i=x.indexOf("=");d[x.slice(0,i)]=x.slice(i+1)}d.BACKUP_AGE_RECIPIENT=process.env.BACKUP_AGE_RECIPIENT;process.stdout.write(JSON.stringify({data:d}))' "$secrets" |
	v write "secret/data/ticket/$env/app" - >/dev/null
node -e 'const f=require("fs");process.stdout.write(JSON.stringify({data:{"tls.crt":f.readFileSync(process.argv[1],"utf8"),"tls.key":f.readFileSync(process.argv[2],"utf8")}}))' "$tls/tls.crt" "$tls/tls.key" |
	v write "secret/data/ticket/$env/tls" - >/dev/null

# T3.12: the Cosign key is a transit key; its private half never leaves Vault. Signers get a short-lived token with
# policy cosign-sign, which may only sign and verify with it (push-images.sh skips images signed already) and read its
# public half. Kyverno checks signatures against that public half, published as ConfigMap kyverno/cosign-public-key
# (a rotated key needs every image signed again).
v secrets list -format=json </dev/null | grep -q '"transit/"' || v secrets enable transit </dev/null
v read transit/keys/cosign </dev/null >/dev/null 2>&1 || v write -f transit/keys/cosign type=ecdsa-p256 </dev/null >/dev/null
printf 'path "transit/sign/cosign" {\n  capabilities = ["update"]\n}\npath "transit/sign/cosign/*" {\n  capabilities = ["update"]\n}\npath "transit/verify/cosign" {\n  capabilities = ["update"]\n}\npath "transit/verify/cosign/*" {\n  capabilities = ["update"]\n}\npath "transit/keys/cosign" {\n  capabilities = ["read"]\n}\n' |
	v policy write cosign-sign - >/dev/null
k create namespace kyverno --dry-run=client -o yaml | k apply -f - >/dev/null
pub=$(mktemp -d)
v read -format=json transit/keys/cosign </dev/null |
	node -e 'const d=JSON.parse(require("fs").readFileSync(0,"utf8")).data;process.stdout.write(d.keys[d.latest_version].public_key)' >"$pub/cosign.pub"
k -n kyverno create configmap cosign-public-key --from-file="$(cygpath -m "$pub/cosign.pub" 2>/dev/null || echo "$pub/cosign.pub")" \
	--dry-run=client -o yaml | k apply --server-side -f - >/dev/null
rm -rf "$pub"
# The CI robot's Harbor login (deploy/local/harbor-setup.sh), for CI to read with its own Vault role (T3.15).
ci=$([ ! -e deploy/local/harbor-robots.json ] ||
	node -e 'const r=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).ci;if(r)process.stdout.write(JSON.stringify({data:{username:r.username,password:r.secret}}))' deploy/local/harbor-robots.json)
[ -z "$ci" ] || printf '%s' "$ci" | v write secret/data/ticket/ci/harbor - >/dev/null

# T3.15: CI signs in with GitHub's own OIDC token (JWT auth), so no Vault secret is stored in GitHub. Only the job of
# .github/workflows/cd-local.yml on main of this repository, on a self-hosted runner, gets role ci: sign and verify
# with the Cosign key and read the Harbor robot's login, for 15 minutes.
repo=bell77m/case-ticket-sys
v auth list -format=json </dev/null | grep -q '"jwt-github/"' || v auth enable -path=jwt-github jwt </dev/null
v write auth/jwt-github/config oidc_discovery_url=https://token.actions.githubusercontent.com \
	bound_issuer=https://token.actions.githubusercontent.com </dev/null >/dev/null
printf 'path "secret/data/ticket/ci/harbor" {\n  capabilities = ["read"]\n}\n' | v policy write ci-harbor - >/dev/null
printf '{"role_type":"jwt","user_claim":"sub","bound_audiences":["vault"],"bound_claims":{"repository":"%s","ref":"refs/heads/main","job_workflow_ref":"%s/.github/workflows/cd-local.yml@refs/heads/main","runner_environment":"self-hosted"},"token_policies":["cosign-sign","ci-harbor"],"token_ttl":"15m","token_max_ttl":"30m"}' "$repo" "$repo" |
	v write auth/jwt-github/role/ci - >/dev/null

echo "Vault ready: sealed=$(v0 status -format=json </dev/null | field sealed), secrets at secret/ticket/$env/{app,tls}"
