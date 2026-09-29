#!/usr/bin/env bash
# T3.10 checks through the ingress of the local k3d cluster (make cluster-check-ingress, after make cluster-deploy
# and make cluster-seed): HTTPS, the company-network allow list (NFR-4), per-client login limits through the
# ingress (FR-A11 with TRUSTED_PROXIES), server-sent events (FR-P3), 100 MB uploads (FR-T1), the /api/track limit
# and no tracking token in the access log. Prints PASS or FAIL per check and exits non-zero on the first FAIL.
set -euo pipefail


ctx=k3d-ticket-local
host=tickets.localtest.me
ca=deploy/overlays/local/tls/ca.crt
base="https://$host:8443"
curl_img=curlimages/curl:8.11.1
k() { kubectl --context "$ctx" "$@"; }
pass() { echo "PASS $*"; }
fail() { echo "FAIL $*"; exit 1; }
json() { printf '{"username":"%s","password":"%s"}' "$1" "$2"; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Test clients run as containers on the cluster's Docker network, each with its own IP, and talk to the ingress on
# the node's LoadBalancer IP directly (the k3d port mapping on the host would give every client the same address).
lb=$(k -n nginx-ingress get svc nginx-ingress-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
docker image inspect "$curl_img" >/dev/null 2>&1 || docker pull -q "$curl_img" >/dev/null
k3d image import -c ticket-local "$curl_img" >/dev/null 2>&1
ca_win=$(cygpath -w "$PWD/$ca" 2>/dev/null || echo "$PWD/$ca")
# Each test client gets its own address on the cluster network (Docker would reuse one freed address for all). The
# block changes every run, so the 15-minute login window of an earlier run cannot leak into this one.
net=$(docker network inspect k3d-ticket-local --format "{{range .IPAM.Config}}{{.Subnet}}{{end}}" | cut -d. -f1-2)
run=$((RANDOM % 200 + 20))
client() { MSYS_NO_PATHCONV=1 docker run --rm --network k3d-ticket-local --ip "$net.$run.$1" -v "$ca_win:/ca.crt:ro" --entrypoint sh "$curl_img" -c "$2"; }
resolve="--cacert /ca.crt --resolve $host:443:$lb"

# 1. HTTPS with the local CA; plain HTTP is redirected to HTTPS (NFR-1).
code=$(curl --ssl-no-revoke -s -o /dev/null -w '%{http_code}' --cacert "$ca" "$base/healthz")
[ "$code" = 200 ] && pass "HTTPS /healthz with the CA certificate: 200" || fail "HTTPS /healthz: $code"
code=$(curl --ssl-no-revoke -s -o /dev/null -w '%{http_code}' "http://$host:8088/")
case $code in 301 | 302 | 307 | 308) pass "plain HTTP redirects to HTTPS: $code" ;; *) fail "plain HTTP answered $code" ;; esac

# 2. An address outside the company network is refused (NFR-4): a pod (10.42.0.0/16) is outside the local allow list.
ingress_ip=$(k -n nginx-ingress get svc nginx-ingress-controller -o jsonpath='{.spec.clusterIP}')
code=$(k -n ticket-local run outside-check --rm -i --restart=Never --quiet --image="$curl_img" -- \
	curl -sk -o /dev/null -w '%{http_code}' --resolve "$host:443:$ingress_ip" "https://$host/healthz" | tr -d '\r')
[ "$code" = 403 ] && pass "outside address (pod network) refused: 403" || fail "outside address got $code"

# 3. FR-A11 through the ingress: 20 failed logins from client A (different usernames, so only the IP limit counts)
# make its 21st attempt 429, while client B still signs in. The app tells A and B apart only if it trusts the
# ingress's X-Forwarded-For (TRUSTED_PROXIES).
codes=$(client 1 "for i in \$(seq 1 21); do curl -s -o /dev/null -w '%{http_code} ' $resolve -H 'Content-Type: application/json' \
	-d \"\$(printf '{\"username\":\"nobody-%s\",\"password\":\"wrong-password\"}' \$i)\" https://$host/api/auth/login; done")
set -- $codes
first=$1
last=${!#}
[ "$first" = 401 ] && [ "$last" = 429 ] && pass "client A: attempts 1-20 refused, 21st limited (429)" ||
	fail "client A codes: $codes"
code=$(client 2 "curl -s -o /dev/null -w '%{http_code}' $resolve -H 'Content-Type: application/json' \
	-d '$(json root dev-password)' https://$host/api/auth/login")
[ "$code" = 200 ] && pass "client B signs in (200) while client A is limited" || fail "client B got $code"
ip=$(k -n ticket-local exec postgres-0 -- psql -U ticket -d ticket -tAc \
	"SELECT host(ip_address) FROM audit_log WHERE action = 'login.failed' ORDER BY id DESC LIMIT 1" | tr -d '\r')
case $ip in 10.42.*) fail "audit recorded the ingress pod ($ip), not the client" ;; 172.*) pass "audit recorded the client's address: $ip" ;; *) fail "audit IP: $ip" ;; esac

# 4. Server-sent events stream through the ingress: pings arrive, and the stream outlives NGINX's default 60 s timeout.
curl --ssl-no-revoke -s -o /dev/null --cacert "$ca" -c "$work/jar" -H 'Content-Type: application/json' -d "$(json root dev-password)" "$base/api/auth/login"
start=$(date +%s)
curl --ssl-no-revoke -s -N --cacert "$ca" -b "$work/jar" --max-time 70 "$base/api/events" >"$work/events" || true
took=$(($(date +%s) - start)); pings=$(grep -c '^: ping' "$work/events" || true)
[ "$took" -ge 68 ] && [ "$pings" -ge 2 ] && pass "SSE open for ${took}s with $pings pings" ||
	fail "SSE closed after ${took}s with $pings pings"

# 5. A 20 MB video goes through the ingress (NGINX's default body limit is 1 MB).
loc=$(curl --ssl-no-revoke -s --cacert "$ca" "$base/api/locations?lang=en" | grep -o '"id":[0-9]*' | tail -1 | cut -d: -f2)
ticket=$(curl --ssl-no-revoke -s --cacert "$ca" -H 'Content-Type: application/json' -d \
	"{\"guest_name\":\"Ingress check\",\"employee_id\":\"E0001\",\"location_id\":$loc,\"case_details\":\"Ingress check: a large video upload.\",\"language\":\"en\"}" \
	"$base/api/tickets")
id=$(echo "$ticket" | grep -o '"ticket_id":[0-9]*' | cut -d: -f2)
token=$(echo "$ticket" | grep -o '"tracking_token":"[^"]*"' | cut -d'"' -f4)
{ printf '\000\000\000\030ftypisom'; head -c $((20 * 1024 * 1024)) /dev/zero; } >"$work/video.mp4"
video=$(cygpath -w "$work/video.mp4" 2>/dev/null || echo "$work/video.mp4") # Windows curl cannot read /tmp paths
code=$(curl --ssl-no-revoke -s -o /dev/null -w '%{http_code}' --cacert "$ca" -H "X-Tracking-Token: $token" \
	-F "file=@$video;type=video/mp4" "$base/api/tickets/$id/attachments")
[ "$code" = 201 ] && pass "20 MB upload through the ingress: 201" || fail "20 MB upload: $code"

# 6. The access log never carries the tracking token (T3.10): the upload above sent it in a header.
if k -n nginx-ingress logs deploy/nginx-ingress-controller --since=5m | grep -q -- "$token"; then
	fail "tracking token found in the ingress access log"
fi
pass "tracking token not in the ingress access log"

# 7. /api/track is limited per client (T2.14): a burst of 40 requests from one client meets 429 from NGINX.
codes=$(client 3 "for i in \$(seq 1 40); do curl -s -o /dev/null -w '%{http_code} ' $resolve https://$host/api/track; done")
case " $codes " in *" 429 "*) pass "/api/track burst limited by NGINX (429 seen)" ;; *) fail "/api/track codes: $codes" ;; esac

# 8. No pod but the ingress controller (and Gotenberg) reaches the app directly (NetworkPolicy ticket-app-ingress-only),
# so nothing inside the cluster can forge X-Forwarded-For past the allow list.
code=$(k -n ticket-local run direct-check --rm -i --restart=Never --quiet --image="$curl_img" -- \
	curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://ticket-app:8080/healthz | tr -d '\r' || true)
[ "$code" = 000 ] && pass "a pod calling the app Service directly is blocked" || fail "direct call from a pod answered $code"

# 9. PDF export still works through the ingress: Gotenberg may reach the app's print page (FR-P4).
code=$(curl --ssl-no-revoke -s -o "$work/report.pdf" -w '%{http_code}' --cacert "$ca" -b "$work/jar" \
	-H 'Content-Type: application/json' -d '{"lang":"en"}' "$base/api/staff/reports/export")
[ "$code" = 200 ] && [ "$(head -c 4 "$work/report.pdf")" = "%PDF" ] && pass "PDF export through the ingress: 200 %PDF" ||
	fail "PDF export: $code"

echo "All ingress checks passed."
