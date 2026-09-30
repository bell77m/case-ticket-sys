#!/usr/bin/env bash
# Try the one-run installer on a PC: a privileged Ubuntu 26.04 container with systemd stands in for a fresh server
# (k3s runs inside it, as in k3d). It installs the committed HEAD of this checkout. Needs about 6 GiB free in Docker:
# stop the local k3d cluster first (k3d cluster stop ticket-local), start it again afterwards.
#
#   deploy/install/test-in-docker.sh [--github-token-file FILE]   install; the app answers on https://127.0.0.1:9443
#   deploy/install/test-in-docker.sh --down                        remove the container and its volumes
#
# With --github-token-file the installer adds its read-only deploy key to the repository; remove it afterwards
# (Settings, Deploy keys, "argocd prod ticket-install-test").
set -euo pipefail
export MSYS_NO_PATHCONV=1
name=ticket-install-test
vols=(ticket-install-test-docker ticket-install-test-rancher ticket-install-test-kubelet)
if [ "${1:-}" = --down ]; then
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker volume rm "${vols[@]}" >/dev/null 2>&1 || true
	exit 0
fi
token=
[ "${1:-}" != --github-token-file ] || token=${2:?}

docker build -q -t ticket-install-test:26.04 - >/dev/null <<'EOF'
FROM ubuntu:26.04
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends systemd systemd-sysv dbus iproute2 iptables kmod \
    git ca-certificates curl openssh-client && apt-get clean && rm -rf /var/lib/apt/lists/*
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
EOF
# Docker and k3s data on volumes: overlayfs cannot sit on the container's own overlay root.
docker run -d --name "$name" --hostname "$name" --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
	--tmpfs /run --tmpfs /run/lock -v "${vols[0]}:/var/lib/docker" -v "${vols[1]}:/var/lib/rancher" \
	-v "${vols[2]}:/var/lib/kubelet" -p 127.0.0.1:9443:443 ticket-install-test:26.04 >/dev/null
for _ in $(seq 30); do docker exec "$name" systemctl is-system-running 2>/dev/null | grep -qE 'running|degraded' && break; sleep 2; done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bundle=$(cygpath -m "$work/repo.bundle" 2>/dev/null || echo "$work/repo.bundle") # Windows git and docker take C:/... paths
git bundle create "$bundle" HEAD >/dev/null 2>&1
docker cp "$bundle" "$name:/root/repo.bundle"
docker exec "$name" sh -c 'rm -rf /opt/case-ticket-sys && git clone -q /root/repo.bundle /opt/case-ticket-sys'
args=(--domain tickets.test.internal --root-admin it-admin)
if [ -n "$token" ]; then
	docker cp "$(cygpath -m "$token" 2>/dev/null || echo "$token")" "$name:/root/gh-token"
	docker exec "$name" chmod 600 /root/gh-token
	args+=(--github-token-file /root/gh-token)
fi
docker exec "$name" bash /opt/case-ticket-sys/deploy/install/install.sh "${args[@]}"
docker exec "$name" rm -f /root/gh-token
