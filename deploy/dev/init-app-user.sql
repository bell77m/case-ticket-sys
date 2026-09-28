-- Dev only (docker compose runs it on a fresh volume). Creates the login the app uses.
-- Tables are owned by "ticket" (runs migrations); ticket_app gets only what migrations grant it (FR-L2).
-- In staging and production the DBA creates ticket_app with a password from Vault.
CREATE ROLE ticket_app LOGIN PASSWORD 'ticket_app';
