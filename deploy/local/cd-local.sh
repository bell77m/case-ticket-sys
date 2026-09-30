#!/usr/bin/env bash
# T3.15 on the local k3d cluster, run by .github/workflows/cd-local.yml on the self-hosted runner (make cluster-runner):
#   cd-local.sh build <refs-file>   build the three images of commit $SHA, scan them, push them to the local Harbor, sign
#                                   them and attest their SBOMs with the Cosign key in Vault; write name=ref lines
#   cd-local.sh pin <refs-file>     pin those digests in deploy/overlays/local (Argo CD syncs it to ticket-local)
#
# No secret is stored in GitHub or on the runner: the job's GitHub OIDC token (audience "vault") logs in to Vault
# role ci (deploy/local/vault-setup.sh), which may only sign with the transit key and read the Harbor robot's login.
# The runner's .env gives the platform's addresses: TICKET_LOCAL_NODE (k3d node container), TICKET_LOCAL_CA (the
# local CA file) and TICKET_LOCAL_VAULT (Vault's ClusterIP URL). Crane, cosign and vault run in the node's network
# namespace, as deploy/local/push-images.sh does; heavy steps run one at a time (Docker's VM has 7.4 GiB).
set -euo pipefail
export MSYS_NO_PATHCONV=1
umask 077
mode=${1:?usage: cd-local.sh build|pin <refs-file>} refs_file=${2:?usage: cd-local.sh build|pin <refs-file>}

if [ "$mode" = pin ]; then
	while IFS='=' read -r name ref; do
		REF=$ref docker run --rm -e REF -v "$(pwd -W 2>/dev/null || pwd):/workdir" mikefarah/yq:4@sha256:cfc4eee658595834ef304eadb0c3ea721f3b7cb6404ad8b7cb909cc5b5145b23 -i 			"(.images[] | select(.name == \"$name\")) |= (.newName = (env(REF) | split(\"@\") | .[0]) | .digest = (env(REF) | split(\"@\") | .[1]) | del(.newTag))" 			deploy/overlays/local/kustomization.yaml
	done <"$refs_file"
	exit 0
fi
: "${SHA:?}" "${TICKET_LOCAL_NODE:?}" "${TICKET_LOCAL_CA:?}" "${TICKET_LOCAL_VAULT:?}"
: "${ACTIONS_ID_TOKEN_REQUEST_URL:?needs permissions: id-token: write}" "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:?}"
harbor=harbor.localtest.me
tag=sha-${SHA:0:12}
# Tool images by digest (the tag is for people): a moved tag cannot change what builds, scans or signs. To update one,
# docker pull the new tag and copy its digest from docker image inspect.
vault_img=hashicorp/vault:2.0.4@sha256:5be49781ecf78bfe775c5309c6a4d9f4e9e040b6c885c99eb2b12fb69855e1a2
crane_img=gcr.io/go-containerregistry/crane:debug@sha256:e78770b31258a3846f878036d9c1f63fbe4c871f9f56990bf77fd95c013e3c1b
cosign_img=ghcr.io/sigstore/cosign/cosign:v2.6.5@sha256:ad281047f85c5e1fc6ffbc30c2b55be3b07b4032bef715a12122ce5829619aca # the version Kyverno's check was proven with (classic .sig tags)
syft_img=anchore/syft:v1.33.0@sha256:f94e5d9fce1f2278491a8e3a63bd5f6ddb81fdfdbb8bf7a1637565c1d5344357
trivy_img=ghcr.io/aquasecurity/trivy:0.58.1@sha256:ab70a02200597efa04748f210f793936eb647cbcdb0ea69cc30b226d6f5a22c7
win() { cygpath -m "$1" 2>/dev/null || echo "$1"; }
work=$(mktemp -d)
cp "$TICKET_LOCAL_CA" "$work/ca.crt"
netrun() {
	docker run --rm -i --network "container:$TICKET_LOCAL_NODE" -v "$(win "$work"):/w" -e DOCKER_CONFIG=/w \
		-e SSL_CERT_FILE=/w/ca.crt -e VAULT_ADDR="$TICKET_LOCAL_VAULT" -e VAULT_TLS_SERVER_NAME=vault.vault.svc \
		-e VAULT_CACERT=/w/ca.crt -e VAULT_TOKEN "$@"
}
vault() { netrun "$vault_img" vault "$@"; }
json() { node -e 'const j=JSON.parse(require("fs").readFileSync(0,"utf8"));let x=j;for(const k of process.argv[1].split("."))x=x?.[k];process.stdout.write(x===undefined?"":String(x))' "$1"; }

echo "== Vault login with the job's GitHub OIDC token"
jwt=$(curl -sSf -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=vault" | json value)
export VAULT_TOKEN
VAULT_TOKEN=$(printf '%s' "$jwt" | vault write -field=token auth/jwt-github/login role=ci jwt=-)
unset jwt
echo "::add-mask::$VAULT_TOKEN"
trap 'vault token revoke -self >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
# Harbor login for crane and cosign, as a Docker config file in the private work folder.
vault kv get -format=json secret/ticket/ci/harbor |
	node -e 'const d=JSON.parse(require("fs").readFileSync(0,"utf8")).data.data;process.stdout.write(JSON.stringify({auths:{[process.argv[1]]:{auth:Buffer.from(d.username+":"+d.password).toString("base64")}}}))' "$harbor" \
	>"$work/config.json"

: >"$refs_file"
for spec in "app:Dockerfile" "migrate:deploy/migrate.Dockerfile" "backup:deploy/backup.Dockerfile"; do
	name=ticket-${spec%%:*}
	img=$harbor/ticket/$name:$tag
	echo "== $name: build, scan, push, sign, attest"
	docker build -q -f "${spec#*:}" -t "$img" . >/dev/null
	# A HIGH or CRITICAL finding with a fix available stops the release, as in cd.yml.
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v ticket-trivy-cache:/root/.cache "$trivy_img" \
		image --quiet --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 "$img"
	docker save "$img" | netrun --entrypoint sh "$crane_img" -c 'cat >/tmp/i.tar && crane push /tmp/i.tar "$0" >/dev/null' "$img"
	digest=$(netrun --entrypoint crane "$crane_img" digest "$img")
	ref=$harbor/ticket/$name@$digest
	docker run --rm -v /var/run/docker.sock:/var/run/docker.sock "$syft_img" -q "$img" -o spdx-json >"$work/sbom.json"
	netrun "$cosign_img" sign --yes --tlog-upload=false --key hashivault://cosign "$ref" >/dev/null
	netrun "$cosign_img" attest --yes --tlog-upload=false --key hashivault://cosign --type spdxjson \
		--predicate /w/sbom.json "$ref" >/dev/null
	netrun "$cosign_img" verify --key hashivault://cosign --insecure-ignore-tlog "$ref" >/dev/null
	echo "signed $ref"
	echo "$name=$ref" >>"$refs_file"
	docker image rm "$img" >/dev/null
done

