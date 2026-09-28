-- Default roles, permissions (docs/REQUIREMENTS.md, FR-R2) and categories (FR-I4).
-- Needed in every environment, so this is a migration, not seed data.
-- Category translations are drafts; native speakers review them in T3.07.

-- +goose Up
INSERT INTO roles (name) VALUES ('Root Admin'), ('Admin'), ('Team Lead'), ('Agent'), ('Viewer');

INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
FROM roles r
JOIN (VALUES
    ('Root Admin', 'ticket.view_all'), ('Root Admin', 'ticket.comment'), ('Root Admin', 'ticket.update'),
    ('Root Admin', 'ticket.assign'), ('Root Admin', 'ticket.delete'), ('Root Admin', 'report.view'),
    ('Root Admin', 'audit.view'), ('Root Admin', 'category.manage'), ('Root Admin', 'staff.manage'),
    ('Root Admin', 'staff.create'), ('Root Admin', 'role.manage'),

    ('Admin', 'ticket.view_all'), ('Admin', 'ticket.comment'), ('Admin', 'ticket.update'),
    ('Admin', 'ticket.assign'), ('Admin', 'ticket.delete'), ('Admin', 'report.view'),
    ('Admin', 'audit.view'), ('Admin', 'category.manage'), ('Admin', 'staff.manage'),

    ('Team Lead', 'ticket.view_all'), ('Team Lead', 'ticket.comment'), ('Team Lead', 'ticket.update'),
    ('Team Lead', 'ticket.assign'), ('Team Lead', 'report.view'), ('Team Lead', 'audit.view'),
    ('Team Lead', 'category.manage'),

    -- Agent may assign only to themselves; the app enforces "self only".
    ('Agent', 'ticket.view_all'), ('Agent', 'ticket.comment'), ('Agent', 'ticket.update'),
    ('Agent', 'ticket.assign'),

    ('Viewer', 'ticket.view_all'), ('Viewer', 'report.view')
) AS p (role, permission) ON p.role = r.name;

INSERT INTO categories (name) VALUES
    ('{"en": "Hardware", "zh-CN": "硬件", "my": "ဟာ့ဒ်ဝဲ", "th": "ฮาร์ดแวร์"}'),
    ('{"en": "Software", "zh-CN": "软件", "my": "ဆော့ဖ်ဝဲ", "th": "ซอฟต์แวร์"}'),
    ('{"en": "Network", "zh-CN": "网络", "my": "ကွန်ရက်", "th": "เครือข่าย"}'),
    ('{"en": "Access", "zh-CN": "访问权限", "my": "ဝင်ရောက်ခွင့်", "th": "สิทธิ์การเข้าถึง"}'),
    ('{"en": "Other", "zh-CN": "其他", "my": "အခြား", "th": "อื่นๆ"}');

-- +goose Down
DELETE FROM categories WHERE name->>'en' IN ('Hardware', 'Software', 'Network', 'Access', 'Other');
DELETE FROM roles WHERE name IN ('Root Admin', 'Admin', 'Team Lead', 'Agent', 'Viewer');
