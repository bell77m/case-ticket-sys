-- Dev-only sample data (T1.06). Safe to run many times: every insert skips existing rows.
-- Real buildings, floors and lines are loaded before the pilot (P.01).

INSERT INTO locations (building, floor, line)
SELECT b.name, f.name, l.name
FROM (VALUES
    ('{"en": "Building A", "zh-CN": "A栋", "my": "အဆောက်အအုံ A", "th": "อาคาร A"}'::jsonb),
    ('{"en": "Building B", "zh-CN": "B栋", "my": "အဆောက်အအုံ B", "th": "อาคาร B"}'::jsonb)
) AS b (name)
CROSS JOIN (VALUES
    ('{"en": "Floor 1", "zh-CN": "1楼", "my": "အထပ် ၁", "th": "ชั้น 1"}'::jsonb),
    ('{"en": "Floor 2", "zh-CN": "2楼", "my": "အထပ် ၂", "th": "ชั้น 2"}'::jsonb)
) AS f (name)
CROSS JOIN (VALUES
    ('{"en": "Line 1", "zh-CN": "1号线", "my": "လိုင်း ၁", "th": "ไลน์ 1"}'::jsonb),
    ('{"en": "Line 2", "zh-CN": "2号线", "my": "လိုင်း ၂", "th": "ไลน์ 2"}'::jsonb)
) AS l (name)
ON CONFLICT (building, floor, line) DO NOTHING;

-- Dev staff: sign in at /login as root, agent or viewer with password `dev-password` (T2.15). One per default role.
-- Re-running resets their passwords.
INSERT INTO staff (name, username, role_id, password_hash, must_change_password)
SELECT s.name, s.username, r.id,
       'pbkdf2-sha256$600000$si7ST8W0IwaGnKrp5X8G6w$SKx5Nau/nN3q3eVLO7dXn5G1hGHab3QYYO/EIdXzVuw', -- gitleaks:allow (dev-only hash of "dev-password")
       false
FROM (VALUES
    ('Dev Root Admin', 'root', 'Root Admin'),
    ('Dev Agent', 'agent', 'Agent'),
    ('Dev Viewer', 'viewer', 'Viewer')
) AS s (name, username, role)
JOIN roles r ON r.name = s.role
ON CONFLICT (username) DO UPDATE
    SET password_hash = EXCLUDED.password_hash,
        must_change_password = false, password_changed_at = now();
