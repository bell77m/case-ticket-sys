---
paths:
  - "deploy/**"
  - "Dockerfile"
  - ".gitlab-ci.yml"
---

# Deploy rules

- Kustomize base + overlays (staging, prod). No Helm for the app itself.
- Containers run as non-root, read-only root filesystem, CPU and memory limits set (Kyverno enforces this).
- Secrets come from Vault via VSO `VaultStaticSecret`. Never put a secret value in a manifest, Dockerfile or CI file.
- Images come only from Harbor and must be Cosign-signed. Kyverno refuses any other Pod outside the platform namespaces (deploy/platform/policies), so a test pod in a script needs a signed Harbor image, `runAsNonRoot` and CPU/memory limits (see `incluster` in deploy/local/ingress-check.sh).
- Scripts run under Git Bash with Windows kubectl and docker: `--from-file=/dev/stdin` fails there, so write secrets to a `mktemp -d` file (umask 077) and pass its `cygpath -m` path.
- `/api/events` Ingress: `proxy_buffering off`, 1 h read timeout. Upload limit `client_max_body_size 100m`.
