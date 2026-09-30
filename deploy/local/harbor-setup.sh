#!/usr/bin/env bash
# T3.12 on the local k3d cluster (make cluster-harbor, after Helm installed Harbor): the private projects "ticket" and
# "dockerhub", two robot accounts, and the pull path for the node and Kyverno. Safe to run again.
#
#   robot$pull  pull only, both projects: containerd on the node (registries.yaml) and Kyverno (Secret harbor-pull)
#   robot$ci    push and pull, both projects: pushes and signs images (make cluster-images; CI in T3.15)
#
# LOCAL ONLY: the robot secrets land in the gitignored deploy/local/harbor-robots.json (vault-setup.sh copies the CI
# robot into Vault at secret/ticket/ci/harbor). On the T3.09 host the admin password comes from Vault and the node's
# registries.yaml is written by ops from Vault; see docs/notes/T3.12.md. Secrets travel on stdin or in files, never as
# command arguments.
set -euo pipefail
export MSYS_NO_PATHCONV=1
umask 077

ctx=k3d-ticket-local
node=k3d-ticket-local-server-0
harbor=harbor.localtest.me
ca=deploy/overlays/local/tls/ca.crt
robots=deploy/local/harbor-robots.json
curl_img=curlimages/curl:8.11.1

k() { kubectl --context "$ctx" "$@"; }
field() { node -e 'const j=JSON.parse(require("fs").readFileSync(0,"utf8"));let x=j;for(const k of process.argv[1].split("."))x=x?.[k];process.stdout.write(x===undefined?"":String(x))' "$1"; }
admin=$(field harborAdminPassword <deploy/local/harbor-secrets.yaml)

# Harbor's API as admin, from the node's network namespace, where harbor.localtest.me (127.0.0.1) is the ingress on
# port 443, the same way containerd reaches it. The password goes to curl as a config file on stdin.
# api METHOD PATH [JSON] prints the body, then the status code on its own last line.
api() {
	local data=(); [ $# -lt 3 ] || data=(-d "$3")
	printf 'user = "admin:%s"\n' "$admin" | docker run --rm -i --network "container:$node" -v "$(win "$PWD/$ca"):/ca.crt:ro" "$curl_img" \
		-sS -K - --cacert /ca.crt -X "$1" -H 'Content-Type: application/json' "${data[@]}" -w '\n%{http_code}' "https://$harbor/api/v2.0$2"
}
code() { tail -n1; }
body() { sed '$d'; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
win() { cygpath -m "$1" 2>/dev/null || echo "$1"; } # kubectl and docker on Windows take C:/... paths

docker image inspect "$curl_img" >/dev/null 2>&1 || docker pull -q "$curl_img" >/dev/null
for _ in $(seq 60); do [ "$(api GET /health | code)" = 200 ] && break; sleep 5; done
[ "$(api GET /health | code)" = 200 ] || { echo "Harbor API not answering at https://$harbor" >&2; exit 1; }

# 1. Private projects. 201 = created, 409 = already there.
for p in ticket dockerhub; do
	c=$(api POST /projects "{\"project_name\":\"$p\",\"metadata\":{\"public\":\"false\",\"auto_scan\":\"true\"}}" | code)
	case $c in 201 | 409) ;; *) echo "create project $p: HTTP $c" >&2; exit 1 ;; esac
done

# 2. Robot accounts. Harbor shows a robot's secret only when it is created, so a robot with no secret on file is
# deleted and made again.
robot() { # name description actions...
	local name=$1 desc=$2; shift 2
	local acc; acc=$(printf '{"resource":"repository","action":"%s"},' "$@"); acc="[${acc%,}]"
	if [ -n "$(field "$name.secret" <"$robots" 2>/dev/null)" ]; then return; fi
	local id; id=$(api GET "/robots?page_size=100" | body | node -e 'const a=JSON.parse(require("fs").readFileSync(0,"utf8"));const r=a.find(x=>x.name.split("$").pop()===process.argv[1]);process.stdout.write(r?String(r.id):"")' "$name")
	[ -z "$id" ] || [ "$(api DELETE "/robots/$id" | code)" = 200 ] || { echo "delete robot $name failed" >&2; exit 1; }
	local out; out=$(api POST /robots "{\"name\":\"$name\",\"description\":\"$desc\",\"level\":\"system\",\"duration\":-1,\"permissions\":[{\"kind\":\"project\",\"namespace\":\"ticket\",\"access\":$acc},{\"kind\":\"project\",\"namespace\":\"dockerhub\",\"access\":$acc}]}")
	[ "$(echo "$out" | code)" = 201 ] || { echo "create robot $name: $(echo "$out" | body)" >&2; exit 1; }
	echo "$out" | body | node -e 'const f=require("fs");const r=JSON.parse(f.readFileSync(0,"utf8"));const p=process.argv[1];const all=f.existsSync(p)?JSON.parse(f.readFileSync(p,"utf8")):{};all[process.argv[2]]={username:r.name,secret:r.secret};f.writeFileSync(p,JSON.stringify(all,null,2)+"\n")' "$robots" "$name"
	echo "robot $name created"
}
[ -e "$robots" ] || echo '{}' >"$robots"
robot pull "Pull only: the cluster's nodes and Kyverno" pull
robot ci "Push and sign images (make cluster-images, CI)" pull push

# 3. Daily rescan of every image, when a scanner is installed (the T3.09 host; the local values turn Trivy off).
if [ "$(api GET /scanners | body | field 0.uuid)" != "" ]; then
	sched='{"schedule":{"type":"Daily","cron":"0 0 2 * * *"}}'
	[ "$(api POST /system/scanAll/schedule "$sched" | code)" = 201 ] || api PUT /system/scanAll/schedule "$sched" >/dev/null
fi

# 4. Pods (Kyverno) resolve harbor.localtest.me to the ingress controller's Service instead of their own 127.0.0.1.
ip=$(k -n nginx-ingress get svc nginx-ingress-controller -o jsonpath='{.spec.clusterIP}')
printf '%s:53 {\n    hosts {\n        %s %s\n    }\n}\n' "$harbor" "$ip" "$harbor" >"$work/harbor.server"
k -n kube-system create configmap coredns-custom --from-file="$(win "$work/harbor.server")" --dry-run=client -o yaml |
	k apply --server-side -f - >/dev/null

# 5. Kyverno pulls signatures with the pull robot (Secret in its namespace, named in the ImageValidatingPolicy).
pull_user=$(field pull.username <"$robots")
pull_secret=$(field pull.secret <"$robots")
k create namespace kyverno --dry-run=client -o yaml | k apply -f - >/dev/null
node -e 'const [u,p,h]=process.argv.slice(1);const a=Buffer.from(u+":"+p).toString("base64");process.stdout.write(JSON.stringify({auths:{[h]:{auth:a}}}))' "$pull_user" "$pull_secret" "$harbor" >"$work/pull.json"
k -n kyverno create secret generic harbor-pull --type=kubernetes.io/dockerconfigjson --from-file=.dockerconfigjson="$(win "$work/pull.json")" \
	--dry-run=client -o yaml | k apply --server-side -f - >/dev/null

# 6. containerd on the node: trust the local CA and log in as the pull robot. k3s reads registries.yaml only when it
# starts, so a changed file restarts the node (which seals Vault: vault-setup.sh unseals it again).
printf 'configs:\n  "%s":\n    auth:\n      username: "%s"\n      password: "%s"\n    tls:\n      ca_file: /etc/rancher/k3s/harbor-ca.crt\n' \
	"$harbor" "$pull_user" "$pull_secret" >"$work/registries.yaml"
if ! docker exec "$node" cat /etc/rancher/k3s/registries.yaml 2>/dev/null | cmp -s - "$work/registries.yaml" ||
	! docker exec "$node" cat /etc/rancher/k3s/harbor-ca.crt 2>/dev/null | cmp -s - "$ca"; then
	docker cp "$(win "$work/registries.yaml")" "$node:/etc/rancher/k3s/registries.yaml"
	docker cp "$(win "$PWD/$ca")" "$node:/etc/rancher/k3s/harbor-ca.crt"
	docker exec "$node" chmod 600 /etc/rancher/k3s/registries.yaml
	echo "restarting $node so containerd reads registries.yaml"
	ip() { docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$node"; }
	old=$(ip)
	docker restart "$node" >/dev/null
	# Docker may give the node another address. k3d's load balancer resolved the old one when it started, and k3s's
	# network policy controller stops k3s while the Node object still names the old one, so fix both.
	docker restart k3d-ticket-local-serverlb >/dev/null
	new=$(ip)
	if [ "$new" != "$old" ]; then
		echo "node moved from $old to $new; updating the Node object"
		for _ in $(seq 120); do
			k patch node "$node" --subresource=status --type=json \
				-p "[{\"op\":\"replace\",\"path\":\"/status/addresses/0/address\",\"value\":\"$new\"}]" >/dev/null 2>&1 &&
				k annotate --overwrite node "$node" "k3s.io/internal-ip=$new" "flannel.alpha.coreos.com/public-ip=$new" >/dev/null 2>&1 &&
				break
			sleep 1
		done
	fi
	for _ in $(seq 60); do k get nodes >/dev/null 2>&1 && break; sleep 5; done
	k wait --for=condition=Ready "node/$node" --timeout=5m >/dev/null
	bash deploy/local/vault-setup.sh
fi

echo "Harbor ready: https://$harbor:8443 (admin password in deploy/local/harbor-secrets.yaml), projects ticket and dockerhub"
