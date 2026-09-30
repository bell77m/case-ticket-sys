# 2026-09-29-argocd-approvers Named Argo CD accounts for production syncs

Date: 2026-09-29 · Requirements: none (release flow in docs/ARCHITECTURE.md; T3.14 follow-up) · Agent: ops (main agent)

## What was done
- Argo CD RBAC (`deploy/platform/argocd-values.yaml`):
  - everyone who signs in may look (`role:readonly`);
  - only `role:prod-approver` may sync applications in project `ticket-prod`.
  So each production release is made by a named person, and Argo CD's log shows who.
- `make cluster-argocd-approver NAME=<name>` adds such a person:
  - the account and its role line go into a second values file (locally the gitignored `deploy/local/argocd-approvers.yaml`), applied with `helm upgrade`, so later upgrades keep them;
  - a random temporary password is bcrypt-hashed and stored in `argocd-secret`, and shown once.

## Files
- deploy/platform/argocd-values.yaml: `configs.rbac` (default read-only, the prod-approver role).
- deploy/local/argocd-approver.sh (new): the account, the values file, the password.
- Makefile: `cluster-argocd` also applies the approvers file when it exists; new `cluster-argocd-approver`.
- .gitignore: `deploy/local/argocd-approvers.yaml`.
- CLAUDE.md, docs/ARCHITECTURE.md: see Docs updated.

## Decisions
- Local accounts, not SSO: there is no SSO for staff (T2.15). Approver group lines go in `policy.approvers.csv`, a second RBAC key Argo CD adds to `policy.csv`, so the role definition and the people stay in separate files.
- `admin` stays enabled for setting up and emergencies (the user's choice). To switch it off once approvers exist, set `configs.cm.admin.enabled: false`.
- The password is hashed by `htpasswd` (`httpd:2.4-alpine`), with the password on stdin. The hash reaches the Secret through a patch file, never a command line. `$2y$` is rewritten to `$2a$`, the same algorithm under the prefix Argo CD writes itself.
- Who the approvers are is still for a human to say. The mechanism does not depend on it.

## Checks
- `make cluster-argocd-approver NAME=approver-test`: helm upgrade ok, account created.
- `argocd admin settings rbac can` (in the argocd-server pod):

  | Subject | Action | Result |
  | --- | --- | --- |
  | approver-test | sync ticket-prod | Yes |
  | approver-test | get ticket-prod | Yes |
  | approver-test | sync ticket-staging | No |
  | approver-test | get ticket-staging | Yes |
  | someone-else | sync ticket-prod | No |
  | someone-else | get ticket-prod | Yes |
  | admin | sync ticket-prod | Yes |

- Sign-in through the Argo CD API (`POST /api/v1/session`): the temporary password gives 200 and a token; a wrong password gives 401. The file holding the password was deleted afterwards.

## Docs updated
- CLAUDE.md: the local cluster line has `make cluster-argocd-approver`.
- docs/ARCHITECTURE.md: the Manifests row describes the RBAC.

## Follow-ups
- Human decision: who the production approvers are.
- T3.09 host: list them in a values file for that cluster, set each password the same way (or with `argocd account update-password`), then consider switching off `admin`.
