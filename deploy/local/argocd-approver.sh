#!/usr/bin/env bash
# make cluster-argocd-approver NAME=<name>: a named Argo CD account that may sync production (role:prod-approver in
# deploy/platform/argocd-values.yaml). The account and its role line go into deploy/local/argocd-approvers.yaml, a
# values file make cluster-argocd applies, so a later helm upgrade keeps them. A random temporary password is printed
# once; the person changes it at first sign-in (UI: User info, Update password). On the T3.09 host the same values
# file lists the real approvers, and each password is set the same way.
set -euo pipefail
umask 077
name=${1:-}
[[ $name =~ ^[a-z][a-z0-9-]{1,30}$ ]] || { echo "NAME: 2-31 lowercase letters, digits or dashes, starting with a letter" >&2; exit 1; }
K="kubectl --context k3d-ticket-local -n argocd"
file=deploy/local/argocd-approvers.yaml

# The values file: one accounts.<name> entry and one group line per approver, in name order.
node -e '
const fs = require("fs"), [file, name] = process.argv.slice(1);
const names = new Set(fs.existsSync(file) ? (fs.readFileSync(file, "utf8").match(/^    accounts\.[a-z0-9-]+:/gm) || []).map((l) => l.trim().slice(9, -1)) : []);
names.add(name);
const list = [...names].sort();
fs.writeFileSync(file, ["# Written by deploy/local/argocd-approver.sh; local cluster only.", "configs:", "  cm:",
  ...list.map((n) => `    accounts.${n}: login`), "  rbac:", "    policy.approvers.csv: |",
  ...list.map((n) => `      g, ${n}, role:prod-approver`), ""].join("\n"));' "$file" "$name"
make --no-print-directory cluster-argocd

# The password: random, bcrypt-hashed by htpasswd with the password on stdin, stored in argocd-secret through a
# patch file (never on a command line). $2y$ and $2a$ are the same algorithm; Argo CD expects $2a$.
pw=$(node -e 'process.stdout.write(require("crypto").randomBytes(12).toString("base64url"))')
hash=$(printf '%s\n' "$pw" | MSYS_NO_PATHCONV=1 docker run --rm -i httpd:2.4-alpine htpasswd -niBC 10 "$name" | cut -d: -f2 | tr -d '\r\n')
patch=$(mktemp)
trap 'rm -f "$patch"' EXIT
printf '{"stringData":{"accounts.%s.password":"%s","accounts.%s.passwordMtime":"%s"}}' \
  "$name" "${hash/\$2y\$/\$2a\$}" "$name" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$patch"
$K patch secret argocd-secret --type merge --patch-file "$patch" >/dev/null
echo "Argo CD account $name may sync ticket-prod. Temporary password (shown once): $pw"
