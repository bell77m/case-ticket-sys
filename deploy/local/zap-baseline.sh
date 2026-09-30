#!/usr/bin/env bash
# DAST stage (T3.15, docs/ARCHITECTURE.md): an OWASP ZAP baseline scan (spider plus passive checks, no attacks) of the
# app on the local k3d cluster, through its ingress. Fails on a high-risk alert; lower alerts are listed only.
# Runs as a plain container on the k3d network, so the ingress sees a client inside the local "company network".
#   zap-baseline.sh [report.json]      (make cluster-zap; cd-local.yml uploads the report)
set -euo pipefail
export MSYS_NO_PATHCONV=1
report=${1:-}
lb=$(docker inspect -f '{{(index .NetworkSettings.Networks "k3d-ticket-local").IPAddress}}' k3d-ticket-local-serverlb)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
chmod 777 "$out" # the ZAP image runs as user zap and writes its report here
docker run --rm --network k3d-ticket-local --add-host "tickets.localtest.me:$lb" \
	-v "$(cygpath -m "$out" 2>/dev/null || echo "$out"):/zap/wrk:rw" ghcr.io/zaproxy/zaproxy:stable \
	zap-baseline.py -t https://tickets.localtest.me/ -m 2 -J zap.json -I >/dev/null || true
[ -s "$out/zap.json" ] || { echo "ZAP wrote no report" >&2; exit 1; }
[ -z "$report" ] || cp "$out/zap.json" "$report"
node -e '
const alerts = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).site.flatMap((s) => s.alerts);
for (const a of alerts) console.log(`${["info", "low", "medium", "high"][a.riskcode]}\t${a.pluginid}\t${a.name} (${a.count})`);
const high = alerts.filter((a) => Number(a.riskcode) >= 3);
console.log(`ZAP baseline: ${alerts.length} alert types, ${high.length} high`);
process.exit(high.length ? 1 : 0);' "$(cygpath -m "$out/zap.json" 2>/dev/null || echo "$out/zap.json")"
