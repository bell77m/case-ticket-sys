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
- Images come only from Harbor and must be Cosign-signed.
- `/api/events` Ingress: `proxy_buffering off`, 1 h read timeout. Upload limit `client_max_body_size 100m`.
