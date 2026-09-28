-- Initial schema: all 9 tables from docs/ARCHITECTURE.md.
-- Requires the role ticket_app to exist (dev: deploy/dev/init-app-user.sql; prod: created by the DBA).

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE roles (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE role_permissions (
    role_id    bigint NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission text   NOT NULL CHECK (permission IN (
        'ticket.view_all', 'ticket.comment', 'ticket.update', 'ticket.assign', 'ticket.delete',
        'report.view', 'audit.view', 'category.manage', 'staff.manage', 'staff.create', 'role.manage')),
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE staff (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL,
    email      text        NOT NULL,
    role_id    bigint      NOT NULL REFERENCES roles (id),
    language   text        NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'zh-CN', 'my', 'th')),
    is_active  boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX staff_email_key ON staff (lower(email));

-- Admin-entered names hold one value per language (FR-I4); English is required as the fallback.
CREATE TABLE categories (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name      jsonb   NOT NULL CHECK (name ? 'en'),
    is_active boolean NOT NULL DEFAULT true
);

CREATE TABLE locations (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    building  jsonb   NOT NULL CHECK (building ? 'en'),
    floor     jsonb   NOT NULL CHECK (floor ? 'en'),
    line      jsonb   NOT NULL CHECK (line ? 'en'),
    is_active boolean NOT NULL DEFAULT true,
    UNIQUE (building, floor, line)
);

CREATE TABLE tickets (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    summary           text        NOT NULL CHECK (char_length(summary) <= 80),
    case_details      text        NOT NULL CHECK (char_length(case_details) BETWEEN 10 AND 5000),
    category_id       bigint      REFERENCES categories (id),
    priority          text        CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    status            text        NOT NULL DEFAULT 'new'
                                  CHECK (status IN ('new', 'in_progress', 'waiting', 'resolved', 'closed')),
    guest_name        text        NOT NULL CHECK (char_length(guest_name) BETWEEN 1 AND 100),
    employee_id       text        NOT NULL CHECK (char_length(employee_id) BETWEEN 1 AND 20),
    language          text        NOT NULL CHECK (language IN ('en', 'zh-CN', 'my', 'th')),
    location_id       bigint      NOT NULL REFERENCES locations (id),
    -- SHA-256 of the guest tracking token; the raw token is never stored (NFR-2).
    access_token_hash bytea       NOT NULL UNIQUE CHECK (octet_length(access_token_hash) = 32),
    assignee_id       bigint      REFERENCES staff (id),
    first_response_at timestamptz,
    resolved_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tickets_status_idx ON tickets (status);
CREATE INDEX tickets_created_at_idx ON tickets (created_at);
CREATE INDEX tickets_location_id_idx ON tickets (location_id);
CREATE INDEX tickets_employee_id_idx ON tickets (employee_id);
-- Trigram search works for Thai, Burmese and Chinese, which have no spaces between words.
CREATE INDEX tickets_case_details_trgm_idx ON tickets USING gin (case_details gin_trgm_ops);

CREATE TABLE comments (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id       bigint      NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    author_staff_id bigint      REFERENCES staff (id), -- NULL = guest
    body            text        NOT NULL CHECK (char_length(body) BETWEEN 1 AND 5000),
    is_internal     boolean     NOT NULL DEFAULT false,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT (is_internal AND author_staff_id IS NULL)) -- guests cannot write internal notes
);
CREATE INDEX comments_ticket_id_idx ON comments (ticket_id);

CREATE TABLE attachments (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticket_id            bigint      NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    file_path            text        NOT NULL,
    media_type           text        NOT NULL CHECK (media_type IN ('image', 'video')),
    size_bytes           bigint      NOT NULL CHECK (size_bytes > 0),
    uploaded_by_staff_id bigint      REFERENCES staff (id), -- NULL = guest
    created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attachments_ticket_id_idx ON attachments (ticket_id);

-- Append-only activity log (FR-L1, FR-L2). ticket_id has no foreign key so entries outlive deleted tickets.
CREATE TABLE audit_log (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_type     text        NOT NULL CHECK (actor_type IN ('staff', 'guest', 'system')),
    actor_staff_id bigint      REFERENCES staff (id),
    action         text        NOT NULL,
    ticket_id      bigint,
    target         text,
    from_value     text,
    to_value       text,
    ip_address     inet,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((actor_type = 'staff') = (actor_staff_id IS NOT NULL))
);
CREATE INDEX audit_log_ticket_id_idx ON audit_log (ticket_id);
CREATE INDEX audit_log_created_at_idx ON audit_log (created_at);

GRANT USAGE ON SCHEMA public TO ticket_app;
-- staff.create and role.manage belong to the Root Admin role only (FR-A3). Enforced here as well as in
-- the app, so a regression in app checks cannot let an Admin raise their own access.
-- +goose StatementBegin
CREATE FUNCTION check_root_only_permission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'role_permissions' THEN
        IF NEW.permission IN ('staff.create', 'role.manage')
           AND (SELECT name FROM roles WHERE id = NEW.role_id) IS DISTINCT FROM 'Root Admin' THEN
            RAISE EXCEPTION 'root_only_permission: % is reserved for Root Admin', NEW.permission;
        END IF;
    ELSIF OLD.name = 'Root Admin' AND NEW.name IS DISTINCT FROM 'Root Admin' THEN
        RAISE EXCEPTION 'root_only_permission: the Root Admin role cannot be renamed';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_permissions_root_only BEFORE INSERT OR UPDATE ON role_permissions
    FOR EACH ROW EXECUTE FUNCTION check_root_only_permission();
CREATE TRIGGER roles_root_name BEFORE UPDATE OF name ON roles
    FOR EACH ROW EXECUTE FUNCTION check_root_only_permission();

GRANT SELECT, INSERT, UPDATE, DELETE
    ON roles, role_permissions, staff, categories, locations, tickets, attachments
    TO ticket_app;
-- Comments are never edited: no UPDATE means an internal note can never become public (FR-G5, FR-T6).
-- Deleting a ticket still removes its comments through ON DELETE CASCADE, which runs as the table owner.
GRANT SELECT, INSERT ON comments TO ticket_app;
GRANT SELECT, INSERT ON audit_log TO ticket_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO ticket_app;

-- +goose Down
DROP TABLE audit_log, attachments, comments, tickets, locations, categories, staff, role_permissions, roles;
DROP FUNCTION IF EXISTS check_root_only_permission();
DROP EXTENSION IF EXISTS pg_trgm;
