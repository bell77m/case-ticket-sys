#!/usr/bin/env bash
# One-run installer for the IT ticket system on a fresh Ubuntu Server 26.04 (T3.09). Run it once as root from a
# checkout of this repository; it builds the whole platform the local k3d cluster proved (docs/ARCHITECTURE.md):
#
#   k3s, F5 NGINX Ingress, Vault (Raft, TLS, audit) with the Vault Secrets Operator, Harbor, Kyverno with the image and
#   pod policies, Argo CD, then the app from Git through Argo CD, the first Root Admin, the nightly app and Vault
#   backups. Images are built on the server from this checkout, pushed to Harbor and signed with the Vault key.
#
#   sudo ./deploy/install/install.sh --config /root/ticket-install.env
#   sudo ./deploy/install/install.sh --domain tickets.example.com --allow 10.0.0.0/8 --github-token-file /root/gh-token
#
# Every step checks first, so a run that stopped half way can simply be started again. Settings: see
# deploy/install/install.env.example; the runbook is docs/INSTALL.md. Secrets are generated here and kept in Vault;
# the few files that must hold one (Vault's unseal keys, the backup key's offline copy, the summary) are root-only and
# listed at the end, to be moved offline. Nothing secret is printed to the terminal or the log.
set -euo pipefail
umask 077

# ---- versions (the ones proven on the local cluster) --------------------------------------------------------------
K3S_VERSION=v1.35.5+k3s1
K3S_INSTALL_SHA256=8598e002e61d658fed7b7542fc6d2c66d8da6eae69e088830105d2ee1ffb6d91 # its install.sh at that tag
HELM_VERSION=v4.3.0
COSIGN_VERSION=v2.6.5
NGINX_CHART=2.7.3
VAULT_CHART=0.34.1
VSO_CHART=1.6.0
HARBOR_CHART=1.19.2
KYVERNO_CHART=3.9.1
ARGOCD_CHART=10.9.2
# Third-party images by digest: a moved tag cannot slip another image past the signing step. To update one,
# `crane digest <name:tag>` (or `docker buildx imagetools inspect`) and replace the digest.
THIRD_PARTY=(
	postgres:16-alpine@sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea
	redis:7-alpine@sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499
	gotenberg/gotenberg:8@sha256:f29984bd1e226bf1b93ba90af06000afa8b315853e99d27b9aaa41b93f15c769
	clamav/clamav:1.5_base@sha256:7769870154c74ce31b0047dd8771e81f7c4269278bc005782e9e419e4922c73d
)

REPO_DIR=$(cd "$(dirname "$0")/../.." && pwd)
STATE=/etc/ticket-install # settings, certificates, generated files; root-only
LOG=/var/log/ticket-install.log
SUMMARY=/root/ticket-install-summary.txt
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export PATH=/usr/local/bin:$PATH

# ---- settings ---------------------------------------------------------------------------------------------------
DOMAIN='' HARBOR_DOMAIN='' ENVIRONMENT=prod ALLOW_CIDRS=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16 ADMIN_CIDRS='' SSH_CIDRS=''
ROOT_ADMIN=it-admin REPO=git@github.com:bell77m/case-ticket-sys.git REVISION=main GITHUB_TOKEN_FILE=''
TLS_CERT='' TLS_KEY='' TLS_CA='' NFS='' SMALL=''
usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; echo "Options: --config FILE, or --domain --harbor-domain --env --allow --admin-cidrs --ssh-cidrs"
	echo "         --root-admin --repo --revision --github-token-file --tls-cert --tls-key --tls-ca --nfs, --small for a test box (see install.env.example)"; exit "${1:-0}"; }
while [ $# -gt 0 ]; do
	case $1 in
	--config) # sourced as root: only a root-owned file no one else may write
		[ -f "$2" ] && [ "$(stat -c %u "$2")" = 0 ] && [ -z "$(find "$2" -perm /022)" ] ||
			{ echo "--config $2 must be a file owned by root and writable only by root" >&2; exit 1; }
		# shellcheck disable=SC1090
		. "$2"; shift 2 ;;
	--domain) DOMAIN=$2; shift 2 ;;
	--harbor-domain) HARBOR_DOMAIN=$2; shift 2 ;;
	--env) ENVIRONMENT=$2; shift 2 ;;
	--allow) ALLOW_CIDRS=$2; shift 2 ;;
	--admin-cidrs) ADMIN_CIDRS=$2; shift 2 ;;
	--ssh-cidrs) SSH_CIDRS=$2; shift 2 ;;
	--root-admin) ROOT_ADMIN=$2; shift 2 ;;
	--repo) REPO=$2; shift 2 ;;
	--revision) REVISION=$2; shift 2 ;;
	--github-token-file) GITHUB_TOKEN_FILE=$2; shift 2 ;;
	--tls-cert) TLS_CERT=$2; shift 2 ;;
	--tls-key) TLS_KEY=$2; shift 2 ;;
	--tls-ca) TLS_CA=$2; shift 2 ;;
	--nfs) NFS=$2; shift 2 ;;
	--small) SMALL=1; shift ;; # test boxes only (test-in-docker.sh): no image scanner, Kyverno admission only
	-h | --help) usage 0 ;;
	*) echo "unknown option $1" >&2; usage 1 ;;
	esac
done
HARBOR_DOMAIN=${HARBOR_DOMAIN:-harbor.$DOMAIN}
# 6443 stays closed unless ADMIN_CIDRS names the admins' networks (kubectl on the server itself always works). SSH
# defaults to the users' networks; the session running this installer is added in step 2, so it is never cut off.
SSH_CIDRS=${SSH_CIDRS:-$ALLOW_CIDRS}
NS=ticket-$ENVIRONMENT
H=$HARBOR_DOMAIN

# ---- helpers ----------------------------------------------------------------------------------------------------
mkdir -p "$STATE" && chmod 700 "$STATE"
touch "$LOG" && chmod 600 "$LOG"
exec > >(tee -a "$LOG") 2>&1
step() { printf '\n== %s  %s\n' "$(date -u +%H:%M:%S)" "$*"; }
say() { printf '   %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
k() { kubectl "$@"; }
rnd() { openssl rand -hex "${1:-16}"; }
json() { jq -r "$1"; }
fetch() { curl -sSfL --retry 5 --retry-all-errors --retry-delay 5 -o "$1" "$2" || die "downloading $2 failed"; } # a link that drops mid-file too
wait_for() { # seconds description command...
	local t=$1 d=$2; shift 2
	for _ in $(seq "$((t / 5))"); do "$@" >/dev/null 2>&1 && return 0; sleep 5; done
	die "timed out after ${t}s waiting for $d"
}
csv_json() { jq -Rc 'split(",") | map(gsub(" "; "")) | map(select(length > 0))' <<<"$1"; }
# A run that was cut off mid-install leaves a Helm release "pending" and every later upgrade refuses. Clear it.
unstick() { # release namespace
	case $(helm status "$1" -n "$2" -o json 2>/dev/null | jq -r '.info.status // empty') in
	pending-install) say "clearing the half-finished install of $1"; helm uninstall "$1" -n "$2" --wait >/dev/null ;;
	pending-upgrade | pending-rollback) say "rolling back the half-finished upgrade of $1"; helm rollback "$1" -n "$2" --wait >/dev/null ;;
	esac
}
# A first install on a slow link can pass a Deployment's 10-minute progress deadline while its images download, and
# Helm 4 then fails at once. The images are there by then, so one more try finishes.
chart() { # release namespace helm-args...
	local r=$1 n=$2; shift 2
	unstick "$r" "$n"
	helm upgrade --install "$r" "$@" -n "$n" >/dev/null && return 0
	say "$r is not ready yet (slow image downloads?): trying once more"
	unstick "$r" "$n"
	helm upgrade --install "$r" "$@" -n "$n" >/dev/null
}

# ---- 1. checks --------------------------------------------------------------------------------------------------
step "1/14 checks"
[ "$EUID" = 0 ] || die "run as root (sudo)"
[ -n "$DOMAIN" ] || usage 1
[[ $DOMAIN =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && $HARBOR_DOMAIN =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || die "bad domain name"
[[ $ENVIRONMENT == prod || $ENVIRONMENT == staging ]] || die "--env must be prod or staging (deploy/overlays/<env>)"
[[ $ROOT_ADMIN =~ ^[a-z0-9._@-]{3,64}$ ]] || die "--root-admin: 3 to 64 of a-z 0-9 . _ - @"
cidr_re='^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$'
for c in ${ALLOW_CIDRS//,/ } ${ADMIN_CIDRS//,/ } ${SSH_CIDRS//,/ }; do [[ $c =~ $cidr_re ]] || die "not an IPv4 CIDR: $c"; done
[ -n "$ALLOW_CIDRS" ] || die "--allow: the users' networks are needed"
[[ $REPO =~ ^(git@github\.com:|https://github\.com/)[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(\.git)?$ ]] || die "--repo: a GitHub repository URL"
[[ $REVISION =~ ^[A-Za-z0-9._/-]{1,100}$ ]] || die "--revision: a branch, tag or commit"
[ -z "$NFS" ] || [[ $NFS =~ ^[A-Za-z0-9.-]+:/[A-Za-z0-9/_.-]+$ ]] || die "--nfs: server:/path"
# shellcheck disable=SC1091
. /etc/os-release
[ "$ID" = ubuntu ] || die "this installer supports Ubuntu Server only (found $ID)"
[ "$VERSION_ID" = 26.04 ] || say "WARNING: made for Ubuntu 26.04, found $VERSION_ID"
[ "$(uname -m)" = x86_64 ] || die "x86_64 only"
mem=$(awk '/MemTotal/ {print int($2/1048576)}' /proc/meminfo)
[ "$mem" -ge 12 ] || say "WARNING: ${mem} GiB RAM; the platform wants 16 GiB (12 at the very least)"
[ "$(nproc)" -ge 4 ] || say "WARNING: $(nproc) CPUs; the platform wants 4 or more"
disk=$(df -BG --output=avail /var | tail -1 | tr -dc 0-9)
[ "$disk" -ge 80 ] || say "WARNING: ${disk} GiB free in /var; Harbor, backups and uploads want 150 GiB or more"
curl -sSf -o /dev/null https://github.com || die "no HTTPS to github.com: the installer downloads k3s, charts and images"
[ -z "$TLS_CERT$TLS_KEY" ] || { [ -r "$TLS_CERT" ] && [ -r "$TLS_KEY" ] && [ -r "$TLS_CA" ]; } ||
	die "--tls-cert, --tls-key and --tls-ca go together (the company certificate, its key and the CA chain)"
git -C "$REPO_DIR" rev-parse HEAD >/dev/null || die "run from a git checkout of the repository"
[ -z "$(git -C "$REPO_DIR" status --porcelain -- backend frontend deploy Dockerfile 2>/dev/null)" ] ||
	say "WARNING: the checkout has local changes; the images will include them"
say "domain $DOMAIN, Harbor $H, environment $ENVIRONMENT, namespace $NS, users from $ALLOW_CIDRS"

# ---- 2. packages, swap, firewall --------------------------------------------------------------------------------
step "2/14 packages, swap, firewall"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq --no-install-recommends curl ca-certificates openssl git jq ufw age docker.io ${NFS:+nfs-common} >/dev/null
systemctl enable --now docker >/dev/null
# kubelet wants no swap. In a container (deploy/install/test-in-docker.sh) the swap is the host's: leave it.
if systemd-detect-virt --container >/dev/null 2>&1; then say "in a container: swap left to the host"
else swapoff -a && sed -i -E '/^[^#].*\sswap\s/s/^/# ticket-install: /' /etc/fstab; fi
# ufw: users reach 80/443 from their networks, admins 6443 (only with ADMIN_CIDRS); pods and services talk freely
# inside the node. SSH from SSH_CIDRS, plus the address this installer's own SSH session comes from, added before the
# firewall is switched on so that session is never cut off.
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
for c in ${SSH_CIDRS//,/ }; do ufw allow from "$c" to any port 22 proto tcp >/dev/null; done
me=${SSH_CLIENT:-}; me=${me%% *} # sudo drops SSH_CLIENT; who -m still names the terminal's origin
[ -n "$me" ] || me=$(who -m 2>/dev/null | sed -nE 's/.*\(([0-9a-fA-F.:]+)\)$/\1/p')
[[ $me =~ ^[0-9a-fA-F.:]+$ ]] && ufw allow from "$me" to any port 22 proto tcp >/dev/null && say "SSH allowed from this session ($me)" || true
for c in ${ALLOW_CIDRS//,/ }; do ufw allow from "$c" to any port 80,443 proto tcp >/dev/null; done
for c in ${ADMIN_CIDRS//,/ }; do ufw allow from "$c" to any port 6443 proto tcp >/dev/null; done
ufw allow from 10.42.0.0/16 >/dev/null && ufw allow from 10.43.0.0/16 >/dev/null
ufw allow in on cni0 >/dev/null && ufw allow in on flannel.1 >/dev/null
ufw --force enable >/dev/null
say "ufw: $(ufw status | head -1)"

# ---- 3. k3s and Helm (checksums verified) -----------------------------------------------------------------------
step "3/14 k3s $K3S_VERSION and Helm $HELM_VERSION"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if ! k3s --version 2>/dev/null | grep -qF "${K3S_VERSION}"; then
	base=https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION//+/%2B}
	fetch "$tmp/k3s" "$base/k3s" && fetch "$tmp/sums" "$base/sha256sum-amd64.txt"
	(cd "$tmp" && grep ' k3s$' sums | sha256sum -c --quiet) || die "k3s checksum mismatch"
	install -m 755 "$tmp/k3s" /usr/local/bin/k3s
	fetch "$tmp/install.sh" "https://raw.githubusercontent.com/k3s-io/k3s/${K3S_VERSION//+/%2B}/install.sh"
	echo "$K3S_INSTALL_SHA256  $tmp/install.sh" | sha256sum -c --quiet || die "k3s install.sh checksum mismatch"
	INSTALL_K3S_SKIP_DOWNLOAD=true INSTALL_K3S_VERSION=$K3S_VERSION sh "$tmp/install.sh" server \
		--disable=traefik --secrets-encryption --write-kubeconfig-mode=600 --tls-san="$DOMAIN" >/dev/null
fi
wait_for 300 "the k3s API" k get nodes
k wait --for=condition=Ready node --all --timeout=5m >/dev/null
if ! helm version --short 2>/dev/null | grep -qF "$HELM_VERSION"; then
	fetch "$tmp/helm.tgz" "https://get.helm.sh/helm-$HELM_VERSION-linux-amd64.tar.gz"
	fetch "$tmp/helm.sha" "https://get.helm.sh/helm-$HELM_VERSION-linux-amd64.tar.gz.sha256sum"
	[ "$(sha256sum "$tmp/helm.tgz" | cut -d' ' -f1)" = "$(cut -d' ' -f1 "$tmp/helm.sha")" ] || die "helm checksum mismatch"
	tar -xzf "$tmp/helm.tgz" -C "$tmp" && install -m 755 "$tmp/linux-amd64/helm" /usr/local/bin/helm
fi
if ! cosign version 2>/dev/null | grep -qF "${COSIGN_VERSION#v}"; then
	base=https://github.com/sigstore/cosign/releases/download/$COSIGN_VERSION
	fetch "$tmp/cosign-linux-amd64" "$base/cosign-linux-amd64" && fetch "$tmp/cosign.sums" "$base/cosign_checksums.txt"
	(cd "$tmp" && grep ' cosign-linux-amd64$' cosign.sums | sha256sum -c --quiet) || die "cosign checksum mismatch"
	install -m 755 "$tmp/cosign-linux-amd64" /usr/local/bin/cosign
fi
say "$(k3s --version | head -1); helm $(helm version --short); cosign $COSIGN_VERSION"

# ---- 4. certificates --------------------------------------------------------------------------------------------
step "4/14 certificates"
T=$STATE/tls
mkdir -p "$T"
if [ ! -s "$T/internal-ca.crt" ]; then # the installation's own CA: always for Vault, and for the web names without --tls-*
	# Name constraints: the host trusts this CA, so it may only ever vouch for this installation's own names.
	openssl req -x509 -newkey rsa:3072 -nodes -days 3650 -subj "/CN=ticket $ENVIRONMENT internal CA" \
		-addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign" \
		-addext "nameConstraints=critical,permitted;DNS:$DOMAIN,permitted;DNS:$H,permitted;DNS:vault,permitted;DNS:vault.svc,permitted;DNS:vault.svc.cluster.local,permitted;DNS:vault-internal,permitted;IP:127.0.0.1/255.255.255.255" \
		-keyout "$T/internal-ca.key" -out "$T/internal-ca.crt" 2>/dev/null
fi
issue() { # name days SAN...
	local n=$1 days=$2; shift 2
	local san; san=$(printf 'DNS:%s,' "$@"); san=${san%,}; san=${san//DNS:127.0.0.1/IP:127.0.0.1}
	openssl req -newkey rsa:2048 -nodes -subj "/CN=$1" -keyout "$T/$n.key" -out "$T/$n.csr" 2>/dev/null
	printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\n' "$san" >"$T/$n.ext"
	openssl x509 -req -in "$T/$n.csr" -CA "$T/internal-ca.crt" -CAkey "$T/internal-ca.key" -CAcreateserial \
		-days "$days" -extfile "$T/$n.ext" -out "$T/$n.crt" 2>/dev/null
}
[ -s "$T/vault.crt" ] || issue vault 825 vault vault.vault.svc vault.vault.svc.cluster.local vault-0.vault-internal 127.0.0.1
if [ -n "$TLS_CERT" ]; then
	install -m 600 "$TLS_CERT" "$T/web.crt" && install -m 600 "$TLS_KEY" "$T/web.key" && install -m 600 "$TLS_CA" "$T/web-ca.crt"
	for n in "$DOMAIN" "$H"; do openssl x509 -in "$T/web.crt" -noout -checkhost "$n" | grep -q 'does match' ||
		die "the company certificate does not name $n (it must cover $DOMAIN and $H)"; done
else
	[ -s "$T/web.crt" ] || issue web 825 "$DOMAIN" "$H"
	cp "$T/internal-ca.crt" "$T/web-ca.crt"
	say "self-signed: users must trust $T/web-ca.crt (see the summary)"
fi
# The host itself (docker push, cosign, curl) trusts both CAs.
install -m 644 "$T/internal-ca.crt" /usr/local/share/ca-certificates/ticket-internal-ca.crt
install -m 644 "$T/web-ca.crt" /usr/local/share/ca-certificates/ticket-web-ca.crt
update-ca-certificates >/dev/null 2>&1
mkdir -p "/etc/docker/certs.d/$H" && install -m 644 "$T/web-ca.crt" "/etc/docker/certs.d/$H/ca.crt"
# Names this host must resolve before company DNS has them (users need real DNS records: see the summary).
for n in "$DOMAIN" "$H"; do getent hosts "$n" >/dev/null || echo "127.0.0.1 $n # ticket-install" >>/etc/hosts; done

# ---- 5. NGINX Ingress, Vault, Vault Secrets Operator --------------------------------------------------------------
step "5/14 NGINX Ingress, Vault, Vault Secrets Operator"
helm repo add hashicorp https://helm.releases.hashicorp.com --force-update >/dev/null
helm repo add harbor https://helm.goharbor.io --force-update >/dev/null
helm repo add kyverno https://kyverno.github.io/kyverno/ --force-update >/dev/null
helm repo add argo https://argoproj.github.io/argo-helm --force-update >/dev/null
for n in vault vault-secrets-operator-system harbor kyverno argocd "$NS"; do
	k create namespace "$n" --dry-run=client -o yaml | k apply -f - >/dev/null
done
k -n vault create secret generic vault-tls --from-file=tls.crt="$T/vault.crt" --from-file=tls.key="$T/vault.key" \
	--from-file=ca.crt="$T/internal-ca.crt" --dry-run=client -o yaml | k apply --server-side -f - >/dev/null
k -n vault-secrets-operator-system create secret generic vault-ca --from-file=ca.crt="$T/internal-ca.crt" \
	--dry-run=client -o yaml | k apply --server-side -f - >/dev/null
chart nginx-ingress nginx-ingress oci://ghcr.io/nginx/charts/nginx-ingress --version "$NGINX_CHART" --create-namespace \
	-f "$REPO_DIR/deploy/platform/nginx-ingress-values.yaml" --wait --timeout 10m
chart vault vault hashicorp/vault --version "$VAULT_CHART" -f "$REPO_DIR/deploy/platform/vault-values.yaml"
chart vault-secrets-operator vault-secrets-operator-system hashicorp/vault-secrets-operator --version "$VSO_CHART" \
	-f "$REPO_DIR/deploy/platform/vso-values.yaml" --wait --timeout 10m

# ---- 6. Vault: init, unseal, audit, auth, policies, secrets --------------------------------------------------------
step "6/14 Vault"
INIT=$STATE/vault-init.json
v0() { k -n vault exec -i vault-0 -- vault "$@"; }
v() { { printf '%s\n' "$VAULT_TOKEN"; [ -t 0 ] || cat; } | k -n vault exec -i vault-0 -- sh -c 'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault "$@"' vault "$@"; }
unseal() {
	# Not the pod's phase: after a k3s restart it still says Running while the container is gone. exec must work.
	wait_for 600 "the Vault container" k -n vault exec vault-0 -- true
	local rc=0 st; st=$(v0 status -format=json </dev/null 2>/dev/null) || rc=$?
	[ "$rc" = 0 ] || [ "$rc" = 2 ] || die "vault status failed (exit $rc)"
	if [ "$(json .initialized <<<"$st")" != true ]; then
		[ ! -e "$INIT" ] || die "$INIT exists but Vault is not initialised: refusing to overwrite the keys"
		v0 operator init -key-shares=5 -key-threshold=3 -format=json </dev/null >"$INIT.tmp" && mv "$INIT.tmp" "$INIT"
		st=$(v0 status -format=json </dev/null 2>/dev/null) || true
		say "Vault initialised: 5 unseal keys and the root token are in $INIT (see the summary)"
	fi
	if [ "$(json .sealed <<<"$st")" = true ]; then
		for i in 0 1 2; do jq -c "{key: .unseal_keys_b64[$i]}" "$INIT" | v0 write -format=json sys/unseal - >/dev/null; done
	fi
	VAULT_TOKEN=$(json .root_token <"$INIT")
}
unseal
v audit list -format=json </dev/null 2>/dev/null | grep -q '"file/"' || v audit enable file file_path=/vault/audit/audit.log </dev/null >/dev/null
v secrets list -format=json </dev/null | grep -q '"secret/"' || v secrets enable -path=secret kv-v2 </dev/null >/dev/null
v auth list -format=json </dev/null | grep -q '"kubernetes/"' || v auth enable kubernetes </dev/null >/dev/null
v write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc </dev/null >/dev/null
printf 'path "secret/data/ticket/%s/*" {\n  capabilities = ["read"]\n}\n' "$ENVIRONMENT" | v policy write "$NS" - >/dev/null
v write "auth/kubernetes/role/$NS" bound_service_account_names=ticket-app-vault bound_service_account_namespaces="$NS" \
	policies="$NS" audience=vault ttl=1h </dev/null >/dev/null
printf 'path "sys/storage/raft/snapshot" {\n  capabilities = ["read"]\n}\n' | v policy write vault-backup - >/dev/null
v write auth/kubernetes/role/vault-backup bound_service_account_names=vault-backup bound_service_account_namespaces=vault \
	policies=vault-backup audience=vault ttl=15m </dev/null >/dev/null
# Cosign key: a transit key whose private half never leaves Vault; Kyverno checks against its public half.
v secrets list -format=json </dev/null | grep -q '"transit/"' || v secrets enable transit </dev/null >/dev/null
v read transit/keys/cosign </dev/null >/dev/null 2>&1 || v write -f transit/keys/cosign type=ecdsa-p256 </dev/null >/dev/null
printf 'path "transit/sign/cosign" { capabilities = ["update"] }\npath "transit/sign/cosign/*" { capabilities = ["update"] }\npath "transit/verify/cosign" { capabilities = ["update"] }\npath "transit/verify/cosign/*" { capabilities = ["update"] }\npath "transit/keys/cosign" { capabilities = ["read"] }\n' |
	v policy write cosign-sign - >/dev/null
v read -format=json transit/keys/cosign </dev/null | jq -r '.data.keys[.data.latest_version|tostring].public_key' >"$tmp/cosign.pub"
k -n kyverno create configmap cosign-public-key --from-file=cosign.pub="$tmp/cosign.pub" --dry-run=client -o yaml |
	k apply --server-side -f - >/dev/null
# Backup key (age): made once; the identity goes to Vault (outside the app's policy) and once to a root-only file for
# the offline copy; only the recipient reaches the cluster, in the app secret.
cur=$(v kv get -format=json "secret/backup/$ENVIRONMENT" </dev/null 2>&1) && rc=0 || rc=$?
if [ "$rc" = 0 ]; then
	recipient=$(json .data.data.recipient <<<"$cur")
elif grep -q 'No value found at' <<<"$cur"; then
	age-keygen -o "$tmp/age" 2>/dev/null
	recipient=$(age-keygen -y "$tmp/age")
	# Secrets reach jq through its environment (root-only), never its command line (readable in ps by every user).
	ID=$(grep '^AGE-SECRET-KEY-' "$tmp/age") R=$recipient jq -n '{options: {cas: 0}, data: {identity: env.ID, recipient: env.R}}' |
		v write "secret/data/backup/$ENVIRONMENT" - >/dev/null
	install -m 600 "$tmp/age" /root/ticket-backup-identity.txt
	shred -u "$tmp/age"
else die "reading secret/backup/$ENVIRONMENT failed: not making a new backup key"; fi
[[ $recipient == age1* ]] || die "secret/backup/$ENVIRONMENT has no age recipient"
# App secrets: made once. The database passwords are set when PostgreSQL first starts, so they are never replaced.
if ! v kv get "secret/ticket/$ENVIRONMENT/app" </dev/null >/dev/null 2>&1; then
	O=$(rnd) A=$(rnd) R=$(rnd) B=$recipient jq -n '{options: {cas: 0}, data: {
		POSTGRES_PASSWORD: env.O, TICKET_APP_DB_PASSWORD: env.A, REDIS_PASSWORD: env.R, BACKUP_AGE_RECIPIENT: env.B,
		DATABASE_URL: "postgres://ticket_app:\(env.A)@postgres:5432/ticket?sslmode=disable",
		MIGRATE_DATABASE_URL: "postgres://ticket:\(env.O)@postgres:5432/ticket?sslmode=disable",
		REDIS_URL: "redis://:\(env.R)@redis:6379/0"}}' | v write "secret/data/ticket/$ENVIRONMENT/app" - >/dev/null
fi
jq -n --rawfile c "$T/web.crt" --rawfile k "$T/web.key" '{data: {"tls.crt": $c, "tls.key": $k}}' |
	v write "secret/data/ticket/$ENVIRONMENT/tls" - >/dev/null
say "Vault ready: sealed=$(v0 status -format=json </dev/null | json .sealed)"

# ---- 7. Harbor --------------------------------------------------------------------------------------------------
step "7/14 Harbor"
if [ ! -s "$STATE/harbor-secrets.yaml" ]; then
	A=$(rnd 16) S=$(rnd 8) C=$(rnd 8) X=$(rnd 16) J=$(rnd 8) G=$(rnd 8) GP=$(rnd 16) D=$(rnd 16) jq -n '{
		harborAdminPassword: env.A, secretKey: env.S, core: {secret: env.C, xsrfKey: env.X}, jobservice: {secret: env.J},
		registry: {secret: env.G, credentials: {password: env.GP}}, database: {internal: {password: env.D}}}' \
		>"$STATE/harbor-secrets.yaml"
fi
k -n harbor create secret tls harbor-tls --cert="$T/web.crt" --key="$T/web.key" --dry-run=client -o yaml |
	k apply --server-side -f - >/dev/null
chart harbor harbor harbor/harbor --version "$HARBOR_CHART" -f "$REPO_DIR/deploy/platform/harbor-values.yaml" \
	-f "$STATE/harbor-secrets.yaml" ${SMALL:+--set trivy.enabled=false} --set "expose.ingress.hosts.core=$H" --set "externalURL=https://$H" \
	--wait --timeout 20m
admin=$(json .harborAdminPassword <"$STATE/harbor-secrets.yaml")
api() { # METHOD PATH [JSON]: body, then the HTTP status on the last line; the password goes to curl on stdin
	local data=(); [ $# -lt 3 ] || data=(-d "$3")
	printf 'user = "admin:%s"\n' "$admin" | curl -sS -K - -X "$1" -H 'Content-Type: application/json' "${data[@]}" \
		-w '\n%{http_code}' "https://$H/api/v2.0$2"
}
harbor_ok() { curl -s "https://$H/api/v2.0/health" | jq -e '.status == "healthy"' >/dev/null; }
wait_for 600 "Harbor's API" harbor_ok
for p in ticket dockerhub; do
	c=$(api POST /projects "{\"project_name\":\"$p\",\"metadata\":{\"public\":\"false\",\"auto_scan\":\"true\"}}" | tail -1)
	case $c in 201 | 409) ;; *) die "create Harbor project $p: HTTP $c" ;; esac
done
ROBOTS=$STATE/harbor-robots.json
[ -s "$ROBOTS" ] || echo '{}' >"$ROBOTS"
robot() { # name description actions...: a robot's secret is shown only at creation, so one with no secret on file is made again
	local name=$1 desc=$2; shift 2
	[ -z "$(jq -r --arg n "$name" '.[$n].secret // empty' "$ROBOTS")" ] || return 0
	local acc id out; acc=$(printf '{"resource":"repository","action":"%s"},' "$@"); acc="[${acc%,}]"
	id=$(api GET "/robots?page_size=100" | sed '$d' | jq -r --arg n "$name" '.[] | select(.name | endswith("$" + $n)) | .id')
	[ -z "$id" ] || [ "$(api DELETE "/robots/$id" | tail -1)" = 200 ] || die "delete Harbor robot $name"
	out=$(api POST /robots "{\"name\":\"$name\",\"description\":\"$desc\",\"level\":\"system\",\"duration\":-1,\"permissions\":[{\"kind\":\"project\",\"namespace\":\"ticket\",\"access\":$acc},{\"kind\":\"project\",\"namespace\":\"dockerhub\",\"access\":$acc}]}")
	[ "$(tail -1 <<<"$out")" = 201 ] || die "create Harbor robot $name: $(sed '$d' <<<"$out")"
	sed '$d' <<<"$out" | jq --slurpfile all "$ROBOTS" --arg n "$name" '$all[0] + {($n): {username: .name, secret: .secret}}' >"$ROBOTS.tmp"
	mv "$ROBOTS.tmp" "$ROBOTS"
}
robot pull "Pull only: the node and Kyverno" pull
robot ci "Push and sign images (installer, CI)" pull push
sched='{"schedule":{"type":"Daily","cron":"0 0 2 * * *"}}'
[ "$(api POST /system/scanAll/schedule "$sched" | tail -1)" = 201 ] || api PUT /system/scanAll/schedule "$sched" >/dev/null
jq '{data: {username: .ci.username, password: .ci.secret}}' "$ROBOTS" | v write secret/data/ticket/ci/harbor - >/dev/null
# Pods (Kyverno) reach Harbor through the ingress controller's Service.
ip=$(k -n nginx-ingress get svc nginx-ingress-controller -o jsonpath='{.spec.clusterIP}')
printf '%s:53 {\n    hosts {\n        %s %s\n    }\n}\n' "$H" "$ip" "$H" >"$tmp/harbor.server"
k -n kube-system create configmap coredns-custom --from-file=harbor.server="$tmp/harbor.server" --dry-run=client -o yaml |
	k apply --server-side -f - >/dev/null
jq --arg h "$H" '{auths: {($h): {auth: (.pull | "\(.username):\(.secret)" | @base64)}}}' "$ROBOTS" >"$tmp/pull.json"
k -n kyverno create secret generic harbor-pull --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson="$tmp/pull.json" \
	--dry-run=client -o yaml | k apply --server-side -f - >/dev/null
# containerd pulls from Harbor as the pull robot and trusts its CA; k3s reads registries.yaml only at start.
install -m 644 "$T/web-ca.crt" /etc/rancher/k3s/harbor-ca.crt
jq -r --arg h "$H" '"configs:\n  \"\($h)\":\n    auth:\n      username: \"\(.pull.username)\"\n      password: \"\(.pull.secret)\"\n    tls:\n      ca_file: /etc/rancher/k3s/harbor-ca.crt"' "$ROBOTS" >"$tmp/registries.yaml"
if ! cmp -s "$tmp/registries.yaml" /etc/rancher/k3s/registries.yaml; then
	install -m 600 "$tmp/registries.yaml" /etc/rancher/k3s/registries.yaml
	say "restarting k3s so containerd reads registries.yaml (Vault is unsealed again)"
	# A plain `systemctl restart k3s` keeps the running containers, and the new containerd starts a second copy of each
	# pod (Vault's Raft file, PostgreSQL's shared memory and ports then clash). k3s-killall.sh stops them all first.
	/usr/local/bin/k3s-killall.sh >/dev/null 2>&1
	systemctl start k3s
	wait_for 300 "the k3s API" k get nodes
	k wait --for=condition=Ready node --all --timeout=5m >/dev/null
	unseal
	wait_for 600 "Harbor after the restart" harbor_ok # its pods restart too; a push before then gets 502
fi

# ---- 8. images: build, push, sign ---------------------------------------------------------------------------------
step "8/14 images: build from this checkout, push to Harbor, sign with the Vault key"
jq -r '.ci.secret' "$ROBOTS" | docker login "$H" -u "$(jq -r '.ci.username' "$ROBOTS")" --password-stdin >/dev/null
tag=sha-$(git -C "$REPO_DIR" rev-parse --short=12 HEAD)
IMAGES=$STATE/images.env
: >"$IMAGES.tmp"
# The digest Harbor holds for a pushed tag. Docker's own record can differ: from a multi-platform image it may push
# only this platform's manifest yet still report the index digest (Docker 29.8.2 did), which Harbor never received.
digest_of() { # harbor-ref
	local proj=${1#"$H"/}; proj=${proj%%/*}
	local repo=${1#"$H/$proj/"}; repo=${repo%:*}
	api GET "/projects/$proj/repositories/${repo//\//%252F}/artifacts/${1##*:}" | sed '$d' | jq -r '.digest // empty'
}
push() { # local-ref harbor-ref key; retried, since Harbor may still be settling after a restart
	docker tag "$1" "$2"
	for attempt in 1 2 3; do
		docker push -q "$2" >/dev/null && break
		[ "$attempt" -lt 3 ] || die "pushing $2 to Harbor failed 3 times"
		sleep 20
	done
	local d; d=$(digest_of "$2")
	[[ $d == sha256:* ]] || die "Harbor has no digest for $2"
	echo "$3=${2%:*}@$d" >>"$IMAGES.tmp"
}
for spec in "ticket-app:Dockerfile" "ticket-migrate:deploy/migrate.Dockerfile" "ticket-backup:deploy/backup.Dockerfile"; do
	name=${spec%%:*}
	say "building $name"
	# Host network: Docker's bridge can drop large downloads (npm, Go modules) where the MTU is smaller (VPN, cloud,
	# nested Docker); one retry for a mirror's bad moment.
	docker build -q --network host -f "$REPO_DIR/${spec#*:}" -t "$name:$tag" "$REPO_DIR" >/dev/null ||
		docker build -q --network host -f "$REPO_DIR/${spec#*:}" -t "$name:$tag" "$REPO_DIR" >/dev/null
	push "$name:$tag" "$H/ticket/$name:$tag" "$name"
done
for img in "${THIRD_PARTY[@]}"; do # name:tag@sha256:...: pulled by digest, pushed to Harbor under name:tag
	named=${img%@*} repo=${img%%:*}
	[[ $repo == */* ]] || { named=library/$named repo=library/$repo; }
	docker pull -q "${img%%:*}@${img#*@}" >/dev/null
	push "${img%%:*}@${img#*@}" "$H/dockerhub/$named" "harbor.example.internal/dockerhub/$repo"
done
mv "$IMAGES.tmp" "$IMAGES"
# A 15-minute token that may only sign, reaching Vault's ClusterIP (its certificate names vault.vault.svc).
VAULT_TOKEN=$(v token create -policy=cosign-sign -ttl=15m -field=token </dev/null) \
	VAULT_ADDR="https://$(k -n vault get svc vault -o jsonpath='{.spec.clusterIP}'):8200" \
	VAULT_TLS_SERVER_NAME=vault.vault.svc VAULT_CACERT="$T/internal-ca.crt" \
	bash -c 'while IFS="=" read -r _ ref; do
		cosign verify --key hashivault://cosign --insecure-ignore-tlog "$ref" >/dev/null 2>&1 ||
			out=$(cosign sign --yes --tlog-upload=false --key hashivault://cosign "$ref" 2>&1) ||
			{ echo "sign $ref failed: $(tail -1 <<<"$out")" >&2; exit 1; }
		echo "   signed $ref"
	done <"$0"' "$IMAGES"
# Only now: cosign pushes the signatures with Docker's login. Then the push robot's secret leaves /root/.docker.
docker logout "$H" >/dev/null

# ---- 9. Kyverno and the policies --------------------------------------------------------------------------------
step "9/14 Kyverno"
cat "$T/web-ca.crt" "$T/internal-ca.crt" >"$tmp/kyverno-ca.crt"
chart kyverno kyverno kyverno/kyverno --version "$KYVERNO_CHART" -f "$REPO_DIR/deploy/platform/kyverno-values.yaml" \
	${SMALL:+-f "$REPO_DIR/deploy/local/kyverno-local-values.yaml"} --set-file global.caCertificates.data="$tmp/kyverno-ca.crt" --wait --timeout 10m
mkdir -p "$STATE/policies"
# kustomize reads a directory resource only by a relative path (outside its root with LoadRestrictionsNone).
policies=$(realpath --relative-to="$STATE/policies" "$REPO_DIR/deploy/platform/policies")
cat >"$STATE/policies/kustomization.yaml" <<EOF
# Written by deploy/install/install.sh: the policies of deploy/platform/policies with this host's Harbor.
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - $policies
patches:
  - target: { kind: ValidatingPolicy, name: harbor-images-only }
    patch: '[{"op": "replace", "path": "/spec/variables/0/expression", "value": "''$H/''"}]'
  - target: { kind: ImageValidatingPolicy, name: harbor-signed-images }
    patch: '[{"op": "replace", "path": "/spec/matchImageReferences/0/glob", "value": "$H/*"}]'
EOF
wait_for 300 "Kyverno's policy types" k get crd validatingpolicies.policies.kyverno.io imagevalidatingpolicies.policies.kyverno.io
k kustomize --load-restrictor=LoadRestrictionsNone "$STATE/policies" | k apply --server-side -f - >/dev/null

# ---- 10. backup volumes (NFS) -----------------------------------------------------------------------------------
step "10/14 backup volumes"
if [ -n "$NFS" ]; then
	server=${NFS%%:*} export_path=${NFS#*:}
	mkdir -p "$tmp/nfs" && mount -t nfs "$NFS" "$tmp/nfs"
	mkdir -p "$tmp/nfs/ticket-$ENVIRONMENT" "$tmp/nfs/vault" && chown 65532:65532 "$tmp/nfs/ticket-$ENVIRONMENT" && chown 100:1000 "$tmp/nfs/vault"
	chmod 700 "$tmp/nfs/ticket-$ENVIRONMENT" "$tmp/nfs/vault" && umount "$tmp/nfs"
	k apply -f - >/dev/null <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: { name: nfs-backups }
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: Immediate
reclaimPolicy: Retain
---
apiVersion: v1
kind: PersistentVolume
metadata: { name: ticket-backups-$ENVIRONMENT }
spec:
  capacity: { storage: 50Gi }
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: nfs-backups
  nfs: { server: $server, path: $export_path/ticket-$ENVIRONMENT }
  claimRef: { namespace: $NS, name: ticket-backups }
---
apiVersion: v1
kind: PersistentVolume
metadata: { name: vault-backups }
spec:
  capacity: { storage: 5Gi }
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: nfs-backups
  nfs: { server: $server, path: $export_path/vault }
  claimRef: { namespace: vault, name: vault-backups }
EOF
	backup_sc=nfs-backups
	say "backups go to $NFS"
else
	backup_sc=
	say "WARNING: no --nfs: backups stay on this server's disk (local-path); a lost disk loses them too"
fi
if ! k -n vault get pvc vault-backups >/dev/null 2>&1 && [ -n "$backup_sc" ]; then
	sed "s/^  accessModes: \[ReadWriteOnce\]$/  accessModes: [ReadWriteOnce]\n  storageClassName: $backup_sc/" \
		"$REPO_DIR/deploy/platform/vault-backup.yaml" | k apply -f - >/dev/null
else
	k apply -f "$REPO_DIR/deploy/platform/vault-backup.yaml" >/dev/null
fi

# ---- 11. Argo CD and repository access -----------------------------------------------------------------------------
step "11/14 Argo CD"
chart argocd argocd argo/argo-cd --version "$ARGOCD_CHART" -f "$REPO_DIR/deploy/platform/argocd-values.yaml" \
	--wait --timeout 10m
KEY=$STATE/argocd-deploy-key
[ -s "$KEY" ] || ssh-keygen -q -t ed25519 -N "" -C "argocd $ENVIRONMENT $(hostname)" -f "$KEY"
git_ok() { GIT_SSH_COMMAND="ssh -i $KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$STATE/known_hosts" \
	git ls-remote "$REPO" "$REVISION" >/dev/null 2>&1; }
if ! git_ok; then
	slug=$(sed -E 's#^(git@github.com:|https://github.com/)##; s#\.git$##' <<<"$REPO")
	if [ -n "$GITHUB_TOKEN_FILE" ]; then
		jq -n --arg k "$(cat "$KEY.pub")" --arg t "argocd $ENVIRONMENT $(hostname), read-only" '{title: $t, key: $k, read_only: true}' |
			curl -sS -o /dev/null -w '%{http_code}' -K <(printf 'header = "Authorization: Bearer %s"\n' "$(tr -d '\r\n' <"$GITHUB_TOKEN_FILE")") \
				-H 'Accept: application/vnd.github+json' -d @- "https://api.github.com/repos/$slug/keys" | grep -qE '^(201|422)$' ||
			die "adding the deploy key through the GitHub API failed"
	else
		say "Add this READ-ONLY deploy key to https://github.com/$slug/settings/keys (Allow write access: off):"
		say "$(cat "$KEY.pub")"
		say "waiting up to 30 minutes for it ..."
	fi
	wait_for 1800 "the deploy key to work" git_ok
fi
k -n argocd create secret generic repo-ticket --from-literal=type=git --from-literal=url="$REPO" --from-file=sshPrivateKey="$KEY" \
	--dry-run=client -o yaml | k label --local -f - argocd.argoproj.io/secret-type=repository -o yaml | k apply --server-side -f - >/dev/null
k apply -f "$REPO_DIR/deploy/argocd/project.yaml" >/dev/null
# The projects allow this repository (it may be a fork of the one named in project.yaml).
for p in ticket-staging ticket-prod; do
	k -n argocd patch appproject "$p" --type merge -p "$(jq -nc --arg r "$REPO" '{spec: {sourceRepos: [$r]}}')" >/dev/null
done

# ---- 12. the app, through Argo CD ---------------------------------------------------------------------------------
step "12/14 the app ($NS) through Argo CD"
images=$(jq -Rn '[inputs | split("=") | "\(.[0])=\(.[1])"]' <"$IMAGES")
allow=$(csv_json "$ALLOW_CIDRS")
patches=$(jq -n --arg d "$DOMAIN" --argjson allow "$allow" --arg sc "$backup_sc" '[
	{target: {kind: "Ingress"}, patch: ([{op: "replace", path: "/spec/rules/0/host", value: $d}] | tostring)},
	{target: {kind: "Ingress", name: "ticket-app"}, patch: ([{op: "replace", path: "/spec/tls/0/hosts/0", value: $d}] | tostring)},
	{target: {kind: "Policy", name: "company-network"}, patch: ([{op: "replace", path: "/spec/accessControl/allow", value: $allow}] | tostring)},
	{target: {kind: "ConfigMap", name: "ticket-app-config"}, patch: ([{op: "replace", path: "/data/BASE_URL", value: "https://\($d)"}] | tostring)}
	] + (if $sc == "" then [] else [{target: {kind: "PersistentVolumeClaim", name: "ticket-backups"},
		patch: ([{op: "add", path: "/spec/storageClassName", value: $sc}] | tostring)}] end)')
sync='{}'
[ "$ENVIRONMENT" = staging ] && sync='{"automated": {"prune": true, "selfHeal": true}}'
jq -n --arg env "$ENVIRONMENT" --arg ns "$NS" --arg repo "$REPO" --arg rev "$REVISION" --argjson images "$images" \
	--argjson patches "$patches" --argjson auto "$sync" '{apiVersion: "argoproj.io/v1alpha1", kind: "Application",
	metadata: {name: $ns, namespace: "argocd"},
	spec: {project: $ns, source: {repoURL: $repo, targetRevision: $rev, path: "deploy/overlays/\($env)",
		kustomize: {images: $images, patches: $patches}},
		destination: {server: "https://kubernetes.default.svc", namespace: $ns},
		syncPolicy: ($auto + {syncOptions: ["CreateNamespace=true"]})}}' | k apply -f - >/dev/null
# A rerun first stops a sync left unfinished by the run before (it may wait for images that have changed since).
op_idle() { case $(k -n argocd get application "$NS" -o jsonpath='{.status.operationState.phase}') in Running | Terminating) return 1 ;; esac; }
if ! op_idle; then
	k -n argocd patch application "$NS" --type merge -p '{"status": {"operationState": {"phase": "Terminating"}}}' >/dev/null
	wait_for 300 "Argo CD to stop the unfinished sync" op_idle
fi
# First sync. Production syncs only when a person asks (its project denies automatic syncs); this run is that ask.
k -n argocd patch application "$NS" --type merge \
	-p "$(jq -nc --arg r "$REVISION" '{operation: {initiatedBy: {username: "install.sh"}, sync: {revision: $r}}}')" >/dev/null
healthy() { [ "$(k -n argocd get application "$NS" -o jsonpath='{.status.sync.status}/{.status.health.status}')" = Synced/Healthy ]; }
wait_for 1800 "Argo CD to sync $NS (Synced/Healthy)" healthy
k -n "$NS" rollout status deploy/ticket-app --timeout=10m >/dev/null
say "$NS: $(k -n argocd get application "$NS" -o jsonpath='{.status.sync.status} {.status.health.status} at {.status.sync.revision}')"

# ---- 13. first Root Admin ---------------------------------------------------------------------------------------
step "13/14 first Root Admin"
if [ ! -e "$STATE/root-admin.done" ]; then
	out=$(k -n "$NS" exec deploy/ticket-app -- /ticket-app create-root-admin --username "$ROOT_ADMIN" 2>&1) ||
		die "create-root-admin failed: $(grep -v -i password <<<"$out")"
	printf '%s\n' "$out" >"$STATE/root-admin.out"
	touch "$STATE/root-admin.done"
	say "Root Admin $ROOT_ADMIN created; the temporary password is in $SUMMARY"
fi

# ---- 14. checks and summary -------------------------------------------------------------------------------------
step "14/14 checks and summary"
code=$(curl -s -o /dev/null -w '%{http_code}' --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/healthz" || true)
say "https://$DOMAIN/healthz from this server: HTTP $code (403 means this server is outside --allow, which is fine)"
k get vaultstaticsecret -A --no-headers 2>/dev/null | awk '{print "   VSO " $1 "/" $2 " " $NF}'
argo_admin=$(k -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || true)
{
	echo "IT ticket system installed on $(hostname), $(date -u +%F\ %T) UTC, commit $(git -C "$REPO_DIR" rev-parse --short HEAD)"
	echo
	echo "App:        https://$DOMAIN   (Root Admin \"$ROOT_ADMIN\"; the temporary password below is changed at first sign-in)"
	grep -i "password" "$STATE/root-admin.out" 2>/dev/null | sed 's/^/            /'
	echo "Harbor:     https://$H   (admin password: harborAdminPassword in $STATE/harbor-secrets.yaml)"
	echo "Argo CD:    kubectl -n argocd port-forward svc/argocd-server 8080:443, user admin, password: $argo_admin"
	echo "            Change it, then delete Secret argocd-initial-admin-secret; add approvers (docs/notes/2026-09-29-argocd-approvers.md)."
	echo
	echo "Do now:"
	echo " 1. DNS: $DOMAIN and $H -> this server's address, for the users' networks ($ALLOW_CIDRS). This server"
	echo "    itself resolves them through /etc/hosts (lines marked # ticket-install); leave those."
	[ -n "$TLS_CERT" ] || echo " 2. Self-signed: users' browsers must trust $T/web-ca.crt (company CA files: --tls-cert/--tls-key/--tls-ca)."
	echo " 3. Vault: $INIT holds the 5 unseal keys and the root token. Give one key to each of 5 people, keep none here,"
	echo "    and revoke the root token once admins have their own login (vault token revoke). After a reboot Vault is"
	echo "    sealed: 3 people unseal it (docs/RESTORE.md, Vault)."
	echo " 4. Backups: /root/ticket-backup-identity.txt is the only copy outside Vault of the key that opens them. Store it"
	echo "    offline with the unseal keys, then delete the file. $( [ -n "$NFS" ] || echo 'Add --nfs and run again: backups are on this disk.')"
	echo " 5. Once the password and the steps above are done: shred -u $SUMMARY $STATE/root-admin.out"
	echo "    (Harbor's robot accounts never expire: rotate them by deleting their entry in $ROBOTS and running again.)"
} >"$SUMMARY"
chmod 600 "$SUMMARY"
echo
echo "Done: https://$DOMAIN ($NS). Read $SUMMARY (root only): passwords and the steps left for people."
