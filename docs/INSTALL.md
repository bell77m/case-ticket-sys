# Installing on a Linux server

For the ops person setting up the server (T3.09). One script builds the whole platform on a fresh **Ubuntu Server 26.04 LTS**, in one run. It installs the same design the local k3d cluster proved (docs/ARCHITECTURE.md): k3s, NGINX Ingress, Vault, Harbor, Kyverno, Argo CD, then the app itself through Argo CD, with backups.

## Before you start

- **Server:** Ubuntu Server 26.04, x86_64, 4 CPUs or more, 16 GiB RAM (12 at the very least), 150 GiB free under `/var`. A fresh install; nothing else listening on ports 80, 443 or 6443.
- **Network:**
  - The server can reach the internet over HTTPS (GitHub, Docker Hub, the Helm chart sites, Ubuntu's mirrors).
  - Users reach it on 443 from the company networks.
- **Names:** two DNS names pointing at the server, for example `tickets.company.internal` for the app and `harbor.tickets.company.internal` for Harbor. The installer works without the records (it adds them to the server's own `/etc/hosts`), but users need them.
- **Certificate:** the company certificate for both names, its key and the CA chain, as files. Without them the installer makes its own CA, and every user's browser must trust it.
- **GitHub:**
  - A clone of this repository on the server.
  - Either a GitHub token in a file (one that may add deploy keys to the repository), or someone who can add a deploy key while the installer waits.
- **Backups:** an NFS export for the nightly backups (`server:/path`). Without one, backups stay on the server's own disk.

## Run it

```sh
git clone git@github.com:bell77m/case-ticket-sys.git /opt/case-ticket-sys
cp /opt/case-ticket-sys/deploy/install/install.env.example /root/ticket-install.env
nano /root/ticket-install.env        # DOMAIN, ALLOW_CIDRS, certificate files, NFS, ...
sudo /opt/case-ticket-sys/deploy/install/install.sh --config /root/ticket-install.env
```

It takes about 30–45 minutes: mostly downloads and building the images. Output goes to the screen and to `/var/log/ticket-install.log`; no secret is printed to either. If it stops, fix the cause and run the same command again: every step checks what is already done.

## What it does

1. Checks the server; installs packages (Docker, age, ufw …); turns swap off.
2. Firewall (ufw): 80 and 443 only from `ALLOW_CIDRS`; the Kubernetes API (6443) only from `ADMIN_CIDRS`, closed if that is empty; SSH from `SSH_CIDRS` (default `ALLOW_CIDRS`) and from the SSH session running the installer.
3. Installs k3s, Helm and cosign, each checked against its published SHA-256 (k3s's install script against a hash kept in the installer).
4. Certificates: the company ones, or its own CA. Vault always gets a certificate from the installation's own CA, which may only sign this installation's names (X.509 name constraints).
5. NGINX Ingress, Vault and the Vault Secrets Operator.
6. Vault:
   - initialises it (5 unseal keys, 3 needed) and unseals it;
   - turns on its audit log;
   - sets up Kubernetes auth, with a read-only role per environment and one for the Vault backup;
   - creates the Cosign transit key and the backup key (age);
   - generates the app's secrets (database and Redis passwords, the TLS certificate). They live only in Vault.
7. Harbor:
   - private projects `ticket` and `dockerhub`;
   - robot accounts `pull` and `ci`;
   - a nightly rescan;
   - the node's pull login.
8. Images: builds the app, migration and backup images from the checked-out commit, copies PostgreSQL, Redis and Gotenberg, pushes all to Harbor, and signs each with the Vault key.
9. Kyverno with the four policies. Outside the platform, the cluster runs only signed images from this Harbor, never as root, and never without CPU and memory limits.
10. Backup volumes on the NFS export (if given), and Vault's nightly snapshot.
11. Argo CD, with a read-only deploy key for the repository.
12. The app: an Argo CD Application for `deploy/overlays/<env>`, with this server's names, networks and image digests, then the first sync (on prod, the installer's run is that manual sync).
13. The first Root Admin (`ROOT_ADMIN`), with a temporary password.
14. A check of `/healthz`, and the summary.

## After it finishes: do these by hand

The installer writes `/root/ticket-install-summary.txt` (root only). It holds the Root Admin's temporary password and Argo CD's first admin password, and lists these steps:

1. **DNS:** point both names at the server for the users' networks.
2. **Certificate** (self-signed only): give users `/etc/ticket-install/tls/web-ca.crt` to trust, through group policy or MDM.
3. **Vault unseal keys:** `/etc/ticket-install/vault-init.json` holds the 5 keys and the root token.
   - Give one key to each of 5 people and keep none on the server.
   - Revoke the root token once admins have their own login.
   - After a reboot, Vault is sealed until 3 of the 5 people unseal it (docs/RESTORE.md).
4. **Backup key:** `/root/ticket-backup-identity.txt` is the only copy outside Vault of the key that opens the backups. Store it offline with the unseal keys, then delete the file.
5. **Argo CD:** change the admin password, delete Secret `argocd-initial-admin-secret`, and add the production approvers (docs/notes/2026-09-29-argocd-approvers.md).
6. Sign in to the app as the Root Admin and change the password (FR-A8). Then load the real buildings, lines, categories and staff (P.01).
7. Delete the summary and the password file: `shred -u /root/ticket-install-summary.txt /etc/ticket-install/root-admin.out`.

## Later

- **Updates:**
  - on staging, Argo CD syncs every change on `main` by itself;
  - on prod, an approver presses Sync in Argo CD.
  New images come from CI (docs/notes/T3.15.md: a self-hosted runner on this server, signing through Vault JWT auth). Until that runner is set up, run the installer again to build, sign and deploy the checked-out commit.
- **Restore:** docs/RESTORE.md.
- **Trying the installer without a server:** `deploy/install/test-in-docker.sh` runs it in a privileged Ubuntu 26.04 container with systemd (about 6 GiB of Docker memory; stop the local k3d cluster first). It passes `--small` (no Harbor scanner, Kyverno's admission controller only); never use that on a real server.
