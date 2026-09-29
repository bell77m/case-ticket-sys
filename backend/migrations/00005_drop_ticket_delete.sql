-- ticket.delete is removed (FR-R2; decision of 2026-09-29 on the T3.06 question): no requirement builds ticket
-- deletion, so the permission granted nothing. Each role that held it loses it, recorded in audit_log as a
-- role.changed by the system (FR-L1), and the CHECK list no longer accepts it.

-- +goose Up
INSERT INTO audit_log (actor_type, action, target, from_value)
SELECT 'system', 'role.changed', r.name, 'ticket.delete'
FROM roles r JOIN role_permissions p ON p.role_id = r.id
WHERE p.permission = 'ticket.delete';
DELETE FROM role_permissions WHERE permission = 'ticket.delete';
ALTER TABLE role_permissions DROP CONSTRAINT role_permissions_permission_check;
ALTER TABLE role_permissions ADD CONSTRAINT role_permissions_permission_check CHECK (permission IN (
    'ticket.view_all', 'ticket.comment', 'ticket.update', 'ticket.assign',
    'report.view', 'audit.view', 'category.manage', 'staff.manage', 'staff.create', 'role.manage'));

-- +goose Down
ALTER TABLE role_permissions DROP CONSTRAINT role_permissions_permission_check;
ALTER TABLE role_permissions ADD CONSTRAINT role_permissions_permission_check CHECK (permission IN (
    'ticket.view_all', 'ticket.comment', 'ticket.update', 'ticket.assign', 'ticket.delete',
    'report.view', 'audit.view', 'category.manage', 'staff.manage', 'staff.create', 'role.manage'));
-- The two default roles that had it in 00002 get it back.
INSERT INTO role_permissions (role_id, permission)
SELECT id, 'ticket.delete' FROM roles WHERE name IN ('Root Admin', 'Admin')
ON CONFLICT DO NOTHING;
INSERT INTO audit_log (actor_type, action, target, to_value)
SELECT 'system', 'role.changed', name, 'ticket.delete' FROM roles WHERE name IN ('Root Admin', 'Admin');
