#!/usr/bin/env bash
# T3.12 checks on the local k3d cluster (make cluster-check-policies, after make cluster-kyverno and
# make cluster-deploy): Kyverno refuses an unsigned image, an image signed with another key, an image from outside
# Harbor, a root container and a container without limits, and admits a signed Harbor image pinned to its digest.
# Pods are really created (not dry-run) in the scratch namespace policy-check, which the script deletes again.
# Prints PASS or FAIL per check and exits non-zero on the first FAIL.
set -euo pipefail
export MSYS_NO_PATHCONV=1
umask 077

ctx=k3d-ticket-local
node=k3d-ticket-local-server-0
harbor=harbor.localtest.me
ns=policy-check
ca=deploy/overlays/local/tls/ca.crt
cosign_img=ghcr.io/sigstore/cosign/cosign:v2.6.5
k() { kubectl --context "$ctx" "$@"; }
pass() { echo "PASS $*"; }
fail() { echo "FAIL $*"; exit 1; }
win() { cygpath -m "$1" 2>/dev/null || echo "$1"; }
work=$(mktemp -d)
trap 'rm -rf "$work"; k delete namespace "$ns" --ignore-not-found --wait=false >/dev/null 2>&1' EXIT

# pod NAME IMAGE [root|nolimits]: a Pod that meets every rule except the one named.
pod() {
	local sc='{runAsNonRoot: true, runAsUser: 65532}' res='{limits: {cpu: 100m, memory: 64Mi}, requests: {cpu: 10m, memory: 16Mi}}'
	[ "${3:-}" != root ] || sc='{}'
	[ "${3:-}" != nolimits ] || res='{}'
	printf 'apiVersion: v1\nkind: Pod\nmetadata: {name: %s, namespace: %s}\nspec:\n  securityContext: %s\n  containers:\n    - {name: c, image: "%s", resources: %s}\n' \
		"$1" "$ns" "$sc" "$2" "$res"
}
# refused NAME IMAGE [variant] RULE: the Pod is refused, and the message names the policy that refused it.
refused() {
	local out
	if out=$(pod "$1" "$2" "${3:-}" | k create -f - 2>&1); then fail "$1 was admitted: $out"; fi
	echo "$out" | grep -q "$4" || fail "$1 refused, but not by $4: $out"
	pass "$1 refused by $4"
}

# Probe images: two builds that differ from ticket-app only by a label, so each has its own digest. One is pushed
# unsigned; the other is signed with a throwaway key that is not the one in Vault.
for p in unsigned otherkey; do
	printf 'FROM ticket-app:local\nLABEL t3.12-policy-probe=%s\n' "$p" | docker build -q -t "ticket-policy-probe:$p" - >/dev/null
done
other=$(NO_SIGN=1 bash deploy/local/push-images.sh ticket-policy-probe:unsigned ticket-policy-probe:otherkey |
	sed -n 's/^not signed: \(.*policy-probe@sha256:.*\)$/\1/p' | tail -n1)
node -e 'const r=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).ci;process.stdout.write(JSON.stringify({auths:{[process.argv[2]]:{auth:Buffer.from(r.username+":"+r.secret).toString("base64")}}}))' \
	deploy/local/harbor-robots.json "$harbor" >"$work/config.json"
cosign() {
	docker run --rm --network "container:$node" -v "$(win "$work"):/cfg" -v "$(win "$PWD/$ca"):/ca.crt:ro" \
		-e DOCKER_CONFIG=/cfg -e SSL_CERT_FILE=/ca.crt -e COSIGN_PASSWORD= -w /cfg "$cosign_img" "$@"
}
cosign generate-key-pair >/dev/null 2>&1
cosign sign --yes --tlog-upload=false --key cosign.key "$other" >/dev/null 2>&1 || fail "could not sign the other-key probe"

k create namespace "$ns" --dry-run=client -o yaml | k apply -f - >/dev/null

# 1. The done criterion: an unsigned image is refused. Then the other ways round the rules.
refused unsigned "$harbor/ticket/ticket-policy-probe:unsigned" "" harbor-signed-images
refused other-key "$harbor/ticket/ticket-policy-probe:otherkey" "" harbor-signed-images
refused docker-hub docker.io/library/postgres:16-alpine "" harbor-images-only
refused root "$harbor/ticket/ticket-app:local" root require-non-root
refused no-limits "$harbor/ticket/ticket-app:local" nolimits require-limits

# 2. A signed Harbor image that meets every rule is admitted and pinned to the digest Kyverno verified.
image=$(pod signed "$harbor/ticket/ticket-app:local" | k create -f - -o jsonpath='{.spec.containers[0].image}' 2>&1) ||
	fail "signed image refused: $image"
[[ $image == "$harbor/ticket/ticket-app@sha256:"* || $image == "$harbor/ticket/ticket-app:local@sha256:"* ]] &&
	pass "signed image admitted as $image" || fail "signed image admitted but not pinned to a digest: $image"

# 3. `kubectl debug` (an ephemeral container, added through its own subresource) meets the same rules. The restricted
# profile makes the debug container non-root, so each refusal is down to the image alone.
debug() {
	local out
	if out=$(k -n "$ns" debug signed --profile=restricted --image="$2" --attach=false -- true 2>&1); then
		fail "kubectl debug with $1 was admitted: $out"
	fi
	echo "$out" | grep -q "$3" || fail "kubectl debug with $1 refused, but not by $3: $out"
	pass "kubectl debug with $1 refused by $3"
}
debug "a Docker Hub image" docker.io/library/busybox:1.36 harbor-images-only
debug "an unsigned image" "$harbor/ticket/ticket-policy-probe:unsigned" harbor-signed-images

# 4. The app itself runs only such images (finished Job pods from before the policies do not count).
images=$(k -n ticket-local get pods --field-selector=status.phase!=Succeeded,status.phase!=Failed -o jsonpath='{range .items[*]}{range .spec.containers[*]}{.image}{"\n"}{end}{end}' | sort -u)
bad=$(echo "$images" | grep -v "^$harbor/.*@sha256:" || true)
[ -z "$bad" ] && pass "ticket-local runs $(echo "$images" | wc -l) images, all from Harbor by digest" ||
	fail "ticket-local runs images not from Harbor by digest: $bad"
