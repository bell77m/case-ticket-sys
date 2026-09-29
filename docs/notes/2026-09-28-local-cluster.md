# 2026-09-28-local-cluster Local test cluster (k3s in Docker)

Date: 2026-09-28 · Requirements: supports T3.10–T3.16 (no FR) · Agent: ops (main agent)

## What was done
- A local Kubernetes cluster for proving the platform tasks before the real host (T3.09) exists: k3s v1.35.5 in Docker through k3d, one node, bundled Traefik disabled as on the planned host. Host ports 127.0.0.1:8088 and :8443 are mapped to the cluster's load balancer for the ingress (T3.10).
- `deploy/overlays/local` runs the T3.13 base manifests unchanged:
  - the app and migration images are built from this checkout and loaded with `k3d image import`;
  - postgres, redis and gotenberg come from Docker Hub instead of the (not yet existing) Harbor proxy;
  - `ticket-app-secrets` is generated from a gitignored `secrets.env` with random passwords. VSO replaces it in T3.11.
- `make cluster-deploy` does by hand what Argo CD's sync waves do: PostgreSQL and Redis ready first, then the migration Job, then the app rollout.
- First deploy: all pods Running; the migration Job completed in 9 s; both app replicas Ready. Through a port-forward:
  - `/healthz` 200;
  - `/api/categories?lang=th` returns the Thai names from PostgreSQL;
  - `/api/staff/tickets` without a session 401.

## Files
- deploy/local/k3d.yaml (new): cluster config (name, ports, `--disable=traefik`).
- deploy/overlays/local/kustomization.yaml, namespace.yaml (new): the local overlay.
- Makefile: `cluster-up`, `cluster-images`, `cluster-deploy`, `cluster-down`, and the `secrets.env` rule. Every kubectl call names `--context k3d-ticket-local`, so no other cluster is touched.
- .gitignore: `deploy/overlays/local/secrets.env`.
- CLAUDE.md: see Docs updated.

## Decisions
- The user chose k3d (k3s in Docker) over waiting for hardware, 2026-09-28. It proves manifests and platform pieces, not the host: Ubuntu, ufw, the company network and the real CA certificate stay with T3.09 and T3.10 on real hardware.
- Third-party images are imported from the host's Docker instead of pulled in the cluster; the first in-cluster pull of gotenberg (about 1.5 GB) took more than 10 minutes.
- The local overlay does not build on the staging overlay: staging points at GHCR digests that are private, and at Harbor names that do not exist yet.
- k3s version: whatever k3d 5.9.0 ships (v1.35.5+k3s1); pin it in deploy/local/k3d.yaml if a later k3d changes behaviour.

## Checks
- `make cluster-up`: node `k3d-ticket-local-server-0` Ready (v1.35.5+k3s1).
- `kubectl kustomize deploy/overlays/local`: only local and Docker Hub images, one generated Secret, everything in `ticket-local`.
- `make cluster-deploy`: exit 0. `kubectl get pods,jobs`: gotenberg, postgres-0, redis-0, 2× ticket-app Running; job ticket-migrate Complete 1/1.
- The first attempt failed as expected: the Job used up its 3 retries while PostgreSQL was still pulling. The target now waits for PostgreSQL and Redis before running the Job.
- Port-forward checks as above.

## Docs updated
- CLAUDE.md: "Local test cluster" line (targets, context, ports, winget tools, PATH note for Git Bash).

## Follow-ups
- T3.10: ingress-nginx on this cluster, the Ingress for the app, `TRUSTED_PROXIES`, and the SSE and upload settings.
- T3.11, T3.12, T3.14, T3.16 next on the same cluster; each note says what is proven locally and what still needs the real host.
- `create-root-admin` inside the cluster: `kubectl --context k3d-ticket-local -n ticket-local exec deploy/ticket-app -- /ticket-app create-root-admin --username <name>`. It prints a temporary password once.
