.PHONY: dev dev-backend dev-frontend test test-go lint build migrate migrate-down seed

# Dev settings: .env.example defaults, overridden by .env if present.
-include .env.example
-include .env
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

migrate:
	goose -dir backend/migrations postgres "$(MIGRATE_DATABASE_URL)" up

migrate-down:
	goose -dir backend/migrations postgres "$(MIGRATE_DATABASE_URL)" down

# Dev-only sample locations; safe to run twice.
seed:
	docker compose exec -T postgres psql -U ticket -d ticket -v ON_ERROR_STOP=1 -q < backend/seed/dev.sql
