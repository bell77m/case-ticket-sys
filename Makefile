.PHONY: dev dev-backend dev-frontend test test-go lint build migrate migrate-down seed

# Dev settings: .env.example defaults, overridden by .env if present.
-include .env.example
-include .env
# PDF export (FR-P4): Gotenberg from docker compose; its Chromium reaches the Vite server through Docker Desktop.
# Same defaults in frontend/playwright.config.ts.
GOTENBERG_URL ?= http://localhost:3000
PRINT_BASE_URL ?= http://host.docker.internal:5173
export

# Runs the Go API on :8080 and the SvelteKit dev server on :5173 (proxies /api and /healthz).
dev:
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	cd backend && go run ./cmd/ticket-app

dev-frontend:
	cd frontend && npm run dev

test:
	cd backend && go test ./...
	node scripts/check-i18n.mjs

# Go tests with the dev env (DB and Redis tests skip without it), e.g. make test-go PKG=./internal/api RUN=Queue
test-go:
	cd backend && go test $(or $(PKG),./...) -run '$(RUN)' -v

lint:
	cd backend && test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	cd backend && golangci-lint run ./...
	cd frontend && npm run check

build:
	cd frontend && npm run build
	find backend/web/dist -mindepth 1 ! -name README.txt -delete && cp -r frontend/build/. backend/web/dist/
	cd backend && go build -o bin/ticket-app ./cmd/ticket-app

# The app as CI's e2e job runs it: the Go binary serving the built SPA on :8080 (keeps /dev/components for
# summary.spec). Test it with: cd frontend && E2E_BASE_URL=http://localhost:8080 npx playwright test
.PHONY: serve-built
serve-built:
	VITE_SHOW_DEV_PAGES=1 $(MAKE) build
	BASE_URL=http://localhost:8080 GUEST_TICKET_LIMIT=1000 PRINT_BASE_URL=http://host.docker.internal:8080 backend/bin/ticket-app

migrate:
	goose -dir backend/migrations postgres "$(MIGRATE_DATABASE_URL)" up

migrate-down:
	goose -dir backend/migrations postgres "$(MIGRATE_DATABASE_URL)" down

# Dev-only sample locations; safe to run twice.
seed:
	docker compose exec -T postgres psql -U ticket -d ticket -v ON_ERROR_STOP=1 -q < backend/seed/dev.sql

# T3.13: kube-linter and Trivy config (both from Docker) on the rendered staging and prod manifests.
.PHONY: deploy-lint
deploy-lint:
	for o in staging prod; do \
		m=$$(kubectl kustomize deploy/overlays/$$o) || exit 1; \
		printf '%s\n' "$$m" | docker run --rm -i stackrox/kube-linter:v0.8.3 lint --fail-if-no-objects-found - || exit 1; \
		printf '%s\n' "$$m" | MSYS_NO_PATHCONV=1 docker run --rm -i -v "$(CURDIR)/deploy/trivy-data.yaml:/data/trivy-data.yaml:ro" --entrypoint sh \
			aquasec/trivy:0.74.0 -c 'cat > /tmp/$$0.yaml && trivy config --quiet --exit-code 1 --config-data /data /tmp/$$0.yaml' $$o || exit 1; \
	done

# Local test cluster (k3s in Docker via k3d; docs/notes/2026-09-28-local-cluster.md). Every kubectl call names the
# k3d context, so these targets never touch another cluster.
KCTX := --context k3d-ticket-local
.PHONY: cluster-up cluster-platform cluster-vault cluster-images cluster-deploy cluster-down
cluster-up:
	k3d cluster create --config deploy/local/k3d.yaml
	$(MAKE) cluster-platform

# Platform pieces, with the same values files the T3.09 host uses (deploy/platform). Vault is installed without
# --wait: its pod is not Ready until cluster-vault initialises and unseals it.
NIC_CHART_VERSION := 2.7.3
VAULT_CHART_VERSION := 0.34.1
VSO_CHART_VERSION := 1.6.0
HELM_LOCAL := --kube-context k3d-ticket-local --create-namespace
cluster-platform: cluster-vault-tls
	helm upgrade --install nginx-ingress oci://ghcr.io/nginx/charts/nginx-ingress --version $(NIC_CHART_VERSION) \
		$(HELM_LOCAL) -n nginx-ingress -f deploy/platform/nginx-ingress-values.yaml --wait
	helm repo add hashicorp https://helm.releases.hashicorp.com --force-update
	helm upgrade --install vault hashicorp/vault --version $(VAULT_CHART_VERSION) \
		$(HELM_LOCAL) -n vault -f deploy/platform/vault-values.yaml
	helm upgrade --install vault-secrets-operator hashicorp/vault-secrets-operator --version $(VSO_CHART_VERSION) \
		$(HELM_LOCAL) -n vault-secrets-operator-system -f deploy/platform/vso-values.yaml --wait

# Initialise or unseal Vault and load the local secrets into it (T3.11).
cluster-vault: deploy/overlays/local/secrets.env deploy/overlays/local/tls/tls.crt
	bash deploy/local/vault-setup.sh

# Vault's own TLS certificate from the local CA (on the T3.09 host: the company CA), as Secrets vault-tls (namespace
# vault) and vault-ca (VSO's namespace), created before Helm installs them (deploy/platform/vault-values.yaml).
.PHONY: cluster-vault-tls
cluster-vault-tls: deploy/overlays/local/tls/vault.crt
	for n in vault vault-secrets-operator-system; do kubectl $(KCTX) create namespace $$n --dry-run=client -o yaml | kubectl $(KCTX) apply -f - >/dev/null; done
	cd deploy/overlays/local/tls && kubectl $(KCTX) -n vault create secret generic vault-tls --from-file=tls.crt=vault.crt \
		--from-file=tls.key=vault.key --from-file=ca.crt --dry-run=client -o yaml | kubectl $(KCTX) apply --server-side -f -
	cd deploy/overlays/local/tls && kubectl $(KCTX) -n vault-secrets-operator-system create secret generic vault-ca \
		--from-file=ca.crt --dry-run=client -o yaml | kubectl $(KCTX) apply --server-side -f -

deploy/overlays/local/tls/vault.crt: deploy/overlays/local/tls/tls.crt
	MSYS_NO_PATHCONV=1 openssl req -newkey rsa:2048 -nodes -subj "/CN=vault" -keyout $(@D)/vault.key -out $(@D)/vault.csr
	printf 'subjectAltName=DNS:vault,DNS:vault.vault.svc,DNS:vault.vault.svc.cluster.local,DNS:vault-0.vault-internal,IP:127.0.0.1\n' > $(@D)/vault-san.ext
	openssl x509 -req -in $(@D)/vault.csr -CA $(@D)/ca.crt -CAkey $(@D)/ca.key -CAcreateserial -days 365 \
		-extfile $(@D)/vault-san.ext -out $@

# A throwaway local CA and a certificate for tickets.localtest.me (gitignored); curl --cacert tls/ca.crt trusts it.
deploy/overlays/local/tls/tls.crt:
	mkdir -p $(@D)
	MSYS_NO_PATHCONV=1 openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj "/CN=ticket-local test CA" \
		-keyout $(@D)/ca.key -out $(@D)/ca.crt
	MSYS_NO_PATHCONV=1 openssl req -newkey rsa:2048 -nodes -subj "/CN=tickets.localtest.me" -keyout $(@D)/tls.key -out $(@D)/tls.csr
	printf 'subjectAltName=DNS:tickets.localtest.me\n' > $(@D)/san.ext
	openssl x509 -req -in $(@D)/tls.csr -CA $(@D)/ca.crt -CAkey $(@D)/ca.key -CAcreateserial -days 365 \
		-extfile $(@D)/san.ext -out $@

# Third-party images come from the host's Docker (pulled once there), which is far faster than pulling in-cluster.
LOCAL_THIRD_PARTY := postgres:16-alpine redis:7-alpine gotenberg/gotenberg:8
cluster-images:
	docker build -t ticket-app:local .
	docker build -f deploy/migrate.Dockerfile -t ticket-migrate:local .
	for i in $(LOCAL_THIRD_PARTY); do docker image inspect $$i >/dev/null 2>&1 || docker pull $$i || exit 1; done
	k3d image import -c ticket-local ticket-app:local ticket-migrate:local ticket-backup:local $(LOCAL_THIRD_PARTY)

# Random passwords, written once; the file is gitignored and loaded into the local Vault by deploy/local/vault-setup.sh.
deploy/overlays/local/secrets.env:
	node -e 'const r=()=>require("crypto").randomBytes(16).toString("hex");const o=r(),a=r(),d=r();process.stdout.write(["POSTGRES_PASSWORD="+o,"TICKET_APP_DB_PASSWORD="+a,"REDIS_PASSWORD="+d,"DATABASE_URL=postgres://ticket_app:"+a+"@postgres:5432/ticket?sslmode=disable","MIGRATE_DATABASE_URL=postgres://ticket:"+o+"@postgres:5432/ticket?sslmode=disable","REDIS_URL=redis://:"+d+"@redis:6379/0",""].join("\n"))' > $@

# Plain kubectl has no Argo CD sync waves (T3.13), so the order is done by hand: data services ready first, then the
# migration Job (an Argo CD hook; deleted and created again so each deploy runs it), then the app. The images come
# first: cluster-vault makes the backup key with the backup image.
cluster-deploy: cluster-images cluster-vault
	kubectl $(KCTX) -n ticket-local delete job ticket-migrate --ignore-not-found
	kubectl $(KCTX) apply -k deploy/overlays/local
	kubectl $(KCTX) -n ticket-local rollout status statefulset/postgres --timeout=5m
	kubectl $(KCTX) -n ticket-local rollout status statefulset/redis --timeout=5m
	kubectl $(KCTX) -n ticket-local delete job ticket-migrate --ignore-not-found
	kubectl $(KCTX) apply -k deploy/overlays/local
	kubectl $(KCTX) -n ticket-local wait --for=condition=complete job/ticket-migrate --timeout=5m
	kubectl $(KCTX) -n ticket-local rollout restart deploy/ticket-app
	kubectl $(KCTX) -n ticket-local rollout status deploy/ticket-app --timeout=5m

.PHONY: cluster-seed cluster-check-ingress cluster-restore-test
# Dev sample locations and the dev staff (root, agent, viewer / dev-password) in the cluster's database.
cluster-seed:
	kubectl $(KCTX) -n ticket-local exec -i postgres-0 -- psql -U ticket -d ticket -v ON_ERROR_STOP=1 -q < backend/seed/dev.sql
	docker build -f deploy/backup.Dockerfile -t ticket-backup:local .

# T3.10 checks through the ingress (HTTPS, allow list, per-client login limit, SSE, uploads, /api/track limit, logs).
cluster-check-ingress:
	bash deploy/local/ingress-check.sh

# T3.16: back up now, restore into the scratch namespace ticket-restore, compare tickets and evidence (docs/RESTORE.md).
cluster-restore-test:
	bash deploy/local/restore-test.sh

# Vault's nightly Raft snapshot (deploy/platform/vault-backup.yaml; its policy and role come from cluster-vault), and
# its restore test: snapshot now, restore into a throwaway Vault, unseal with the real keys, compare.
.PHONY: cluster-vault-backup cluster-vault-restore-test
cluster-vault-backup: cluster-vault
	kubectl $(KCTX) apply -f deploy/platform/vault-backup.yaml
cluster-vault-restore-test:
	bash deploy/local/vault-restore-test.sh

cluster-down:
	k3d cluster delete ticket-local

# Argo CD (T3.14): install, give it a read-only deploy key for this repo (private half only in a cluster Secret and in
# the gitignored deploy/local/argocd-deploy-key), then the projects and the local Application. The staging and prod
# Applications (deploy/argocd/staging.yaml, prod.yaml) belong on the T3.09 host, not here.
ARGOCD_CHART_VERSION := 10.9.2
ARGOCD_REPO := git@github.com:bell77m/case-ticket-sys.git
.PHONY: cluster-argocd
cluster-argocd: deploy/local/argocd-deploy-key
	helm repo add argo https://argoproj.github.io/argo-helm --force-update
	helm upgrade --install argocd argo/argo-cd --version $(ARGOCD_CHART_VERSION) \
		$(HELM_LOCAL) -n argocd -f deploy/platform/argocd-values.yaml \
		$(if $(wildcard deploy/local/argocd-approvers.yaml),-f deploy/local/argocd-approvers.yaml) --wait
	kubectl $(KCTX) -n argocd create secret generic repo-ticket --from-literal=type=git --from-literal=url=$(ARGOCD_REPO) \
		--from-file=sshPrivateKey=deploy/local/argocd-deploy-key --dry-run=client -o yaml \
		| kubectl label --local -f - argocd.argoproj.io/secret-type=repository -o yaml | kubectl $(KCTX) apply --server-side -f -
	kubectl $(KCTX) apply -f deploy/argocd/project.yaml -f deploy/argocd/local/ticket-local.yaml

# A read-only deploy key, registered once on GitHub (needs `gh` signed in). Revoke it in the repo's Deploy keys page.
deploy/local/argocd-deploy-key:
	ssh-keygen -q -t ed25519 -N "" -C "argocd ticket-local (k3d)" -f $@
	gh repo deploy-key add $@.pub --repo bell77m/case-ticket-sys --title "argocd ticket-local (k3d), read-only"
# A named account that may sync production (role:prod-approver): make cluster-argocd-approver NAME=alice. Prints a
# temporary password once (deploy/local/argocd-approver.sh).
.PHONY: cluster-argocd-approver
cluster-argocd-approver:
	bash deploy/local/argocd-approver.sh $(NAME)

