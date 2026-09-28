package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

// FR-A6, FR-A8: the first run creates the Root Admin with a printed temporary password that signs in and must be
// changed; a second run refuses unless --force; a taken username is refused.
func TestCreateRootAdmin_FRA6(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	// The dev DB already has Root Admins (make seed); work in a transaction that hides them and rolls back.
	// Repeatable read: Root Admins that other packages' tests commit meanwhile (go test runs packages in parallel)
	// stay out of this transaction's view.
	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	defer tx.Rollback()
	if err := tx.Exec(`UPDATE staff SET is_active = false WHERE role_id = (SELECT id FROM roles WHERE name = 'Root Admin')`).Error; err != nil {
		t.Fatal(err)
	}
	user := func(n int) string { return fmt.Sprintf("root%d-%d", n, time.Now().UnixNano()) }

	first := user(1)
	password, err := createRootAdmin(tx, first, "", false)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	var st models.Staff
	if err := tx.Where("username = ? AND is_active", first).First(&st).Error; err != nil {
		t.Fatalf("root admin not created: %v", err)
	}
	var role string
	tx.Raw("SELECT name FROM roles WHERE id = ?", st.RoleID).Scan(&role)
	if role != "Root Admin" || st.Name != first {
		t.Errorf("created role %q name %q, want Root Admin named after the username", role, st.Name)
	}
	if auth.PasswordProblem(password) != "" || !auth.CheckPassword(st.PasswordHash, password) || !st.MustChangePassword {
		t.Errorf("printed password %q: fits policy %q, matches %v, must change %v", password,
			auth.PasswordProblem(password), auth.CheckPassword(st.PasswordHash, password), st.MustChangePassword)
	}
	var audited int64
	tx.Model(&models.AuditEntry{}).Where("action = 'staff.created' AND actor_type = 'system' AND target = ?", first).Count(&audited)
	if audited != 1 {
		t.Errorf("staff.created audit rows = %d, want 1", audited)
	}

	if _, err := createRootAdmin(tx, user(2), "", false); !errors.Is(err, errRootAdminExists) {
		t.Errorf("second run error = %v, want errRootAdminExists", err)
	}
	if _, err := createRootAdmin(tx, user(3), "Second Admin", true); err != nil {
		t.Errorf("second run with --force: %v", err)
	}
	if _, err := createRootAdmin(tx, first, "", true); err == nil {
		t.Errorf("existing username accepted twice")
	}
}

func TestRunCreateRootAdmin_BadArgs_FRA6(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--name", "No Username"},
		{"--username", "okname", "--email", "a@b.c"}, // staff have no email (2026-09-24)
		{"--username", "ab"},
		{"--username", "Has Space"},
	} {
		if code := runCreateRootAdmin(args, func(string) string { return "" }); code != 2 {
			t.Errorf("args %q exit = %d, want 2", args, code)
		}
	}
}
