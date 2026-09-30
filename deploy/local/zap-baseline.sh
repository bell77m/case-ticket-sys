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
name=zap-baseline-$$
# ZAP sometimes stalls (once for 44 minutes; a rerun then took under 6). It must not hold the CD job: -silent stops
# its own calls home (update and news checks), -T caps its wait, timeout caps each attempt, one retry, and the named
# container is removed on the way out.
trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$out"' EXIT
chmod 777 "$out" # the ZAP image runs as user zap and writes its report here
for attempt in 1 2; do
	timeout 600 docker run --rm --name "$name" --network k3d-ticket-local --add-host "tickets.localtest.me:$lb" \
		-v "$(cygpath -m "$out" 2>/dev/null || echo "$out"):/zap/wrk:rw" ghcr.io/zaproxy/zaproxy:stable@sha256:781a2bdaea47324e7bab583e2263f21d257b0aee61ed51521a5be45f5f5081ef \
		zap-baseline.py -t https://tickets.localtest.me/ -m 2 -T 8 -z "-silent" -J zap.json -I >/dev/null || true
	docker rm -f "$name" >/dev/null 2>&1 || true
	[ -s "$out/zap.json" ] && break
	echo "ZAP attempt $attempt wrote no report (stalled or stopped after 10 minutes)" >&2
done
[ -s "$out/zap.json" ] || exit 1
[ -z "$report" ] || cp "$out/zap.json" "$report"
node -e '
const alerts = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).site.flatMap((s) => s.alerts);
for (const a of alerts) console.log(`${["info", "low", "medium", "high"][a.riskcode]}\t${a.pluginid}\t${a.name} (${a.count})`);
const high = alerts.filter((a) => Number(a.riskcode) >= 3);
console.log(`ZAP baseline: ${alerts.length} alert types, ${high.length} high`);
process.exit(high.length ? 1 : 0);' "$(cygpath -m "$out/zap.json" 2>/dev/null || echo "$out/zap.json")"
