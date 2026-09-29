#!/bin/sh
# Runs once, when PostgreSQL initializes an empty volume (same job as deploy/dev/init-app-user.sql).
# Creates the login the app uses; migrations (run as the owner "ticket") grant it only what it needs (FR-L2).
# The password comes from the Vault-fed Secret, never from Git. The entrypoint sources this file under set -e.
# format(%L) + \gexec quotes the password, and keeps Trivy KSV-0109 from mistaking the psql variable for a stored one.
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v pw="$TICKET_APP_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE ticket_app LOGIN PASSWORD %L', :'pw') \gexec
SQL
