package migrations

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"ticket-app/internal/rbac"
)

// Needs the compose DB migrated (make migrate). DATABASE_URL is the app user, not the owner.
func connect(t *testing.T) *pgx.Conn {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// FR-L2: the app user can INSERT and SELECT audit_log, but never UPDATE or DELETE it.
func TestAuditLogAppendOnly_FRL2(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO audit_log (actor_type, action) VALUES ('system', 'test.insert') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert as app user: %v", err)
	}

	for _, stmt := range []string{
		`UPDATE audit_log SET action = 'tampered' WHERE id = $1`,
		`DELETE FROM audit_log WHERE id = $1`,
	} {
		_, err := tx.Exec(ctx, "SAVEPOINT s")
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, stmt, id)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%q: err = %v, want permission denied", stmt, err)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
	}
}

// The app user has normal write access to the other tables.
func TestAppUserCanWriteTickets(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		WITH loc AS (
			INSERT INTO locations (building, floor, line)
			VALUES ('{"en":"B"}', '{"en":"1"}', '{"en":"L1"}') RETURNING id
		)
		INSERT INTO tickets (summary, case_details, guest_name, employee_id, language, location_id, access_token_hash)
		SELECT 'Printer jam', 'Printer jams on page two', 'Aye', 'E123', 'my', id, decode(repeat('ab', 32), 'hex') FROM loc`)
	if err != nil {
		t.Fatalf("insert ticket as app user: %v", err)
	}
}

// FR-G5, FR-T6: the app user cannot UPDATE comments, so an internal note can never be flipped to public.
func TestCommentsCannotBeUpdated_FRG5(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	_, err := conn.Exec(ctx, `UPDATE comments SET is_internal = false WHERE false`)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("UPDATE comments: err = %v, want permission denied", err)
	}
}

// FR-A3: staff.create and role.manage can only belong to the Root Admin role, even if app checks regress.
func TestRootOnlyPermissions_FRA3(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rootID, adminID int64
	if err := tx.QueryRow(ctx, `INSERT INTO roles (name) VALUES ('Root Admin') ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO roles (name) VALUES ('Test Admin FRA3') RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_permissions VALUES ($1, 'staff.create') ON CONFLICT DO NOTHING`, rootID); err != nil {
		t.Fatalf("root grant: %v", err)
	}
	for _, perm := range []string{"staff.create", "role.manage"} {
		if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_permissions VALUES ($1, $2)`, adminID, perm)
		if err == nil || !strings.Contains(err.Error(), "root_only_permission") {
			t.Errorf("grant %s to non-root: err = %v, want root_only_permission", perm, err)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET name = 'Renamed' WHERE id = $1`, rootID); err == nil || !strings.Contains(err.Error(), "root_only_permission") {
		t.Errorf("rename Root Admin: err = %v, want root_only_permission", err)
	}
}

// FR-R2, FR-L1: ticket.delete is gone (no requirement builds ticket deletion; decision of 2026-09-29). No role holds
// it, it can no longer be granted, and its removal from each default role is in the activity log.
func TestTicketDeleteDropped_FRR2(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	var held int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM role_permissions WHERE permission = 'ticket.delete'`).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held != 0 {
		t.Errorf("roles holding ticket.delete = %d, want 0", held)
	}
	var logged []string
	rows, err := conn.Query(ctx, `SELECT DISTINCT target FROM audit_log
		WHERE actor_type = 'system' AND action = 'role.changed' AND from_value = 'ticket.delete' ORDER BY target`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatal(err)
		}
		logged = append(logged, target)
	}
	if !slices.Equal(logged, []string{"Admin", "Root Admin"}) {
		t.Errorf("audited removals = %v, want [Admin Root Admin]", logged)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission)
		SELECT id, 'ticket.delete' FROM roles WHERE name = 'Admin'`)
	if err == nil || !strings.Contains(err.Error(), "role_permissions_permission_check") {
		t.Errorf("grant ticket.delete: err = %v, want role_permissions_permission_check", err)
	}
}

// FR-R2: default roles and permissions match the table in docs/REQUIREMENTS.md.
func TestDefaultRoles_FRR2(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	want := map[string][]string{
		"Root Admin": rbac.All,
		"Admin": {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign",
			"report.view", "audit.view", "category.manage", "staff.manage"},
		"Team Lead": {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign",
			"report.view", "audit.view", "category.manage"},
		"Agent":  {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign"}, // assign: self only, enforced in the app
		"Viewer": {"ticket.view_all", "report.view"},
	}
	rows, err := conn.Query(ctx, `
		SELECT r.name, coalesce(string_agg(p.permission, ',' ORDER BY p.permission), '')
		FROM roles r LEFT JOIN role_permissions p ON p.role_id = r.id GROUP BY r.name`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for rows.Next() {
		var name, perms string
		if err := rows.Scan(&name, &perms); err != nil {
			t.Fatal(err)
		}
		got[name] = perms
	}
	for role, perms := range want {
		sorted := append([]string(nil), perms...)
		slices.Sort(sorted)
		if got[role] != strings.Join(sorted, ",") {
			t.Errorf("%s: permissions = %q, want %q", role, got[role], strings.Join(sorted, ","))
		}
	}
}

// FR-I4: default categories exist with a name in all four languages.
func TestDefaultCategories_FRI4(t *testing.T) {
	conn := connect(t)
	var n int
	err := conn.QueryRow(context.Background(),
		`SELECT count(*) FROM categories WHERE name ?& array['en', 'zh-CN', 'my', 'th']`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatalf("categories with all 4 languages = %d, want >= 5", n)
	}
}

// FR-A1: usernames are lower case from a small character set and unique; email is optional.
func TestStaffUsernameFormat_FRA1(t *testing.T) {
	ctx := context.Background()
	conn := connect(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	insert := func(username string) error {
		if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO staff (name, username, role_id) SELECT 'X', $1, id FROM roles WHERE name = 'Agent'`, username)
		if err != nil {
			if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); rbErr != nil {
				t.Fatal(rbErr)
			}
		}
		return err
	}
	if err := insert("no.email_ok"); err != nil {
		t.Errorf("username without email refused: %v", err)
	}
	for _, bad := range []string{"Upper", "has space", "ab", "plus+sign"} {
		if err := insert(bad); err == nil || !strings.Contains(err.Error(), "staff_username_format") {
			t.Errorf("username %q: err = %v, want staff_username_format violation", bad, err)
		}
	}
	if err := insert("no.email_ok"); err == nil {
		t.Errorf("duplicate username accepted")
	}
}

// FR-A1: staff have no email (decided 2026-09-24: notifications are in-app only, so the app has no use for it).
func TestStaffHasNoEmail_FRA1(t *testing.T) {
	var n int
	err := connect(t).QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.columns WHERE table_name = 'staff' AND column_name = 'email'`).Scan(&n)
	if err != nil || n != 0 {
		t.Errorf("staff.email columns = %d (err %v), want none", n, err)
	}
}
