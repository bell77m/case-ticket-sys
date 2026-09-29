# Image for the goose migration Job (deploy/base/migrate-job.yaml): goose plus backend/migrations, nothing else.
# The app image has neither. Build from the repo root, with the same tag as the app image:
#   docker build -f deploy/migrate.Dockerfile -t ticket-migrate:<tag> .
FROM golang:1.27-alpine AS goose
# Same goose version as CI (.github/workflows/ci.yml); the tags drop the database drivers we do not use.
RUN CGO_ENABLED=0 go install -trimpath -ldflags="-s -w" \
    -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb no_duckdb no_spanner no_starrocks' \
    github.com/pressly/goose/v3/cmd/goose@v3.28.0

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=goose /go/bin/goose /goose
COPY backend/migrations/*.sql /migrations/
USER nonroot:nonroot
ENTRYPOINT ["/goose"]
