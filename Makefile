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
.PHONY: cluster-up cluster-images cluster-deploy cluster-down
cluster-up:
	k3d cluster create --config deploy/local/k3d.yaml

# Third-party images come from the host's Docker (pulled once there), which is far faster than pulling in-cluster.
LOCAL_THIRD_PARTY := postgres:16-alpine redis:7-alpine gotenberg/gotenberg:8
cluster-images:
	docker build -t ticket-app:local .
	docker build -f deploy/migrate.Dockerfile -t ticket-migrate:local .
	for i in $(LOCAL_THIRD_PARTY); do docker image inspect $$i >/dev/null 2>&1 || docker pull $$i || exit 1; done
	k3d image import -c ticket-local ticket-app:local ticket-migrate:local $(LOCAL_THIRD_PARTY)

# Random passwords, written once; the file is gitignored and read by the local overlay's secretGenerator.
deploy/overlays/local/secrets.env:
	node -e 'const r=()=>require("crypto").randomBytes(16).toString("hex");const o=r(),a=r(),d=r();process.stdout.write(["POSTGRES_PASSWORD="+o,"TICKET_APP_DB_PASSWORD="+a,"REDIS_PASSWORD="+d,"DATABASE_URL=postgres://ticket_app:"+a+"@postgres:5432/ticket?sslmode=disable","MIGRATE_DATABASE_URL=postgres://ticket:"+o+"@postgres:5432/ticket?sslmode=disable","REDIS_URL=redis://:"+d+"@redis:6379/0",""].join("\n"))' > $@

# Plain kubectl has no Argo CD sync waves (T3.13), so the order is done by hand: data services ready first, then the
# migration Job (an Argo CD hook; deleted and created again so each deploy runs it), then the app.
cluster-deploy: deploy/overlays/local/secrets.env cluster-images
	kubectl $(KCTX) apply -k deploy/overlays/local
	kubectl $(KCTX) -n ticket-local rollout status statefulset/postgres --timeout=5m
	kubectl $(KCTX) -n ticket-local rollout status statefulset/redis --timeout=5m
	kubectl $(KCTX) -n ticket-local delete job ticket-migrate --ignore-not-found
	kubectl $(KCTX) apply -k deploy/overlays/local
	kubectl $(KCTX) -n ticket-local wait --for=condition=complete job/ticket-migrate --timeout=5m
	kubectl $(KCTX) -n ticket-local rollout restart deploy/ticket-app
	kubectl $(KCTX) -n ticket-local rollout status deploy/ticket-app --timeout=5m

cluster-down:
	k3d cluster delete ticket-local
