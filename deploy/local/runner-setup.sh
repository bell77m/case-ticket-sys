#!/usr/bin/env bash
# make cluster-runner (T3.15): a GitHub Actions self-hosted runner on this PC, label ticket-local, for
# .github/workflows/cd-local.yml only (every other job runs on GitHub's own runners). It runs as the signed-in user,
# in %USERPROFILE%\actions-runner-ticket, and stops at sign-out; run this target again to start it. Safe to rerun.
# The job it runs can use Docker and the local cluster's Harbor and Vault, so keep the repository private and its
# collaborators trusted: a workflow that names this label runs on this PC. Remove with:
#   (cd "$USERPROFILE/actions-runner-ticket" && ./config.cmd remove --token <token from the repo's Runners page>)
set -euo pipefail
repo=bell77m/case-ticket-sys
dir=$(cygpath "$USERPROFILE")/actions-runner-ticket
mkdir -p "$dir"

if [ ! -e "$dir/config.cmd" ]; then
	ver=$(gh api repos/actions/runner/releases/latest --jq .tag_name)
	ver=${ver#v}
	zip=actions-runner-win-x64-$ver.zip
	echo "downloading actions/runner $ver"
	curl -sSfL -o "$dir/runner.zip" "https://github.com/actions/runner/releases/download/v$ver/$zip"
	# Extract only what GitHub published: the release asset's SHA-256 must match the download.
	want=$(gh api repos/actions/runner/releases/latest --jq ".assets[] | select(.name == \"$zip\") | .digest")
	got=sha256:$(sha256sum "$dir/runner.zip" | cut -d' ' -f1)
	[ -n "$want" ] && [ "$got" = "$want" ] || { echo "runner zip checksum mismatch: got $got, want $want" >&2; rm -f "$dir/runner.zip"; exit 1; }
	powershell -NoProfile -Command "Expand-Archive -Path '$(cygpath -w "$dir/runner.zip")' -DestinationPath '$(cygpath -w "$dir")' -Force"
	rm -f "$dir/runner.zip"
fi

# Where the local platform is, for deploy/local/cd-local.sh (the runner reads .env when it starts): the k3d node whose
# network namespace reaches Harbor and Vault, the local CA (gitignored, so not in the job's checkout), Vault's ClusterIP.
vault_ip=$(kubectl --context k3d-ticket-local -n vault get svc vault -o jsonpath='{.spec.clusterIP}')
printf 'TICKET_LOCAL_NODE=k3d-ticket-local-server-0\nTICKET_LOCAL_CA=%s\nTICKET_LOCAL_VAULT=https://%s:8200\n' \
	"$(cygpath -m "$PWD/deploy/overlays/local/tls/ca.crt")" "$vault_ip" >"$dir/.env"

if [ ! -e "$dir/.runner" ]; then
	# A one-hour registration token from the GitHub API (gh is signed in as the repository owner).
	token=$(gh api -X POST "repos/$repo/actions/runners/registration-token" --jq .token)
	(cd "$dir" && ./config.cmd --unattended --url "https://github.com/$repo" --token "$token" \
		--name "$(hostname)-ticket-local" --labels ticket-local --work _work --replace)
fi

if tasklist 2>/dev/null | grep -qi 'Runner.Listener'; then
	echo "runner already running"
else
	powershell -NoProfile -Command "Start-Process -FilePath '$(cygpath -w "$dir/run.cmd")' -WorkingDirectory '$(cygpath -w "$dir")' -WindowStyle Hidden"
	echo "runner started"
fi
