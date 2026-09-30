# Image for the nightly backup and the restores (deploy/base/backup.yaml, deploy/restore/restore-pod.yaml; T3.16):
# PostgreSQL 16 client tools, the same major version as the server, plus age, which encrypts every backup file.
# Build from the repo root, with the same tag as the app image:
#   docker build -f deploy/backup.Dockerfile -t ticket-backup:<tag> .
FROM alpine:3.24
RUN apk add --no-cache postgresql16-client age
USER 65532:65532
