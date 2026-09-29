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
