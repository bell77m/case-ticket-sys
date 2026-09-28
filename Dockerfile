# Multi-stage build: SvelteKit static files embedded in one static Go binary (docs/ARCHITECTURE.md).
FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/frontend/build ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ticket-app ./cmd/ticket-app

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/ticket-app /ticket-app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/ticket-app"]
