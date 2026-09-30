#!/usr/bin/env bash
# T3.12 on the local k3d cluster (make cluster-images): push images from the host's Docker to the local Harbor and
# sign each with the Cosign key in Vault, so Kyverno lets the cluster run them.
#
#   deploy/local/push-images.sh ticket-app:local ... postgres:16-alpine gotenberg/gotenberg:8
#   NO_SIGN=1 deploy/local/push-images.sh ticket-policy-probe:unsigned   (policy-check.sh's unsigned image)
#
# ticket-* images are ours: pushed to project "ticket" every time (the tag moves to the new build). Anything else is
# a third-party copy for project "dockerhub" (Docker Hub's library/ prefix added for official images), pushed only
# when Harbor does not have that tag yet. Every pushed digest is signed unless it already carries a valid signature.
#
# LOCAL ONLY: signing uses a 15-minute token with policy cosign-sign, made from the root token in the gitignored
# deploy/local/vault-init.json; CI will get its own through Vault JWT auth (T3.15). The private key never leaves Vault.
set -euo pipefail
export MSYS_NO_PATHCONV=1
umask 077

ctx=k3d-ticket-local
node=k3d-ticket-local-server-0
harbor=harbor.localtest.me
ca=deploy/overlays/local/tls/ca.crt
crane_img=gcr.io/go-containerregistry/crane:debug
cosign_img=ghcr.io/sigstore/cosign/cosign:v2.6.5

k() { kubectl --context "$ctx" "$@"; }
field() { node -e 'const j=JSON.parse(require("fs").readFileSync(0,"utf8"));let x=j;for(const k of process.argv[1].split("."))x=x?.[k];process.stdout.write(x===undefined?"":String(x))' "$1"; }
win() { cygpath -m "$1" 2>/dev/null || echo "$1"; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Registry login for crane and cosign: the CI robot (deploy/local/harbor-setup.sh), as a Docker config file.
node -e 'const r=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).ci;process.stdout.write(JSON.stringify({auths:{[process.argv[2]]:{auth:Buffer.from(r.username+":"+r.secret).toString("base64")}}}))' \
	deploy/local/harbor-robots.json "$harbor" >"$work/config.json"

# The signing token, made inside the Vault pod with the root token on stdin.
root=$(field root_token <deploy/local/vault-init.json)
export VAULT_TOKEN
VAULT_TOKEN=$(printf '%s\n' "$root" | k -n vault exec -i vault-0 -- sh -c \
	'read -r VAULT_TOKEN; export VAULT_TOKEN; exec vault token create -policy=cosign-sign -ttl=15m -field=token')
vault_ip=$(k -n vault get svc vault -o jsonpath='{.spec.clusterIP}')

# Tools run in the node's network namespace: harbor.localtest.me (127.0.0.1) is the ingress there, and Vault's
# ClusterIP is routable (its certificate names vault.vault.svc, hence VAULT_TLS_SERVER_NAME).
run() {
	docker run --rm -i --network "container:$node" -v "$(win "$work"):/cfg:ro" -v "$(win "$PWD/$ca"):/ca.crt:ro" \
		-e DOCKER_CONFIG=/cfg -e SSL_CERT_FILE=/ca.crt -e VAULT_ADDR="https://$vault_ip:8200" \
		-e VAULT_TLS_SERVER_NAME=vault.vault.svc -e VAULT_CACERT=/ca.crt -e VAULT_TOKEN "$@"
}
crane() { run --entrypoint crane "$crane_img" "$@"; }
cosign() { run "$cosign_img" "$@"; }

for img in "$@"; do
	name=${img%:*}
	case $name in
	ticket-*) ref=$harbor/ticket/$img ;;
	*/*) ref=$harbor/dockerhub/$img ;;
	*) ref=$harbor/dockerhub/library/$img ;;
	esac
	if [[ $name == ticket-* ]] || ! crane digest "$ref" >/dev/null 2>&1; then
		echo "push $img -> $ref"
		docker save "$img" | run --entrypoint sh "$crane_img" -c 'cat >/tmp/i.tar && crane push /tmp/i.tar "$0" >/dev/null' "$ref"
	fi
	digest=$(crane digest "$ref")
	[ "${NO_SIGN:-}" != 1 ] || { echo "not signed: ${ref%:*}@$digest"; continue; }
	if cosign verify --key hashivault://cosign --insecure-ignore-tlog "${ref%:*}@$digest" >/dev/null 2>&1; then
		echo "signed already: ${ref%:*}@$digest"
	else
		out=$(cosign sign --yes --tlog-upload=false --key hashivault://cosign "${ref%:*}@$digest" 2>&1) || { echo "$out" >&2; exit 1; }
		echo "signed: ${ref%:*}@$digest"
	fi
done
