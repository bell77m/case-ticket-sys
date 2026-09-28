package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

var errRootAdminExists = errors.New("an active Root Admin already exists; use --force to add another")

// runCreateRootAdmin is `ticket-app create-root-admin --username <u> [--name <name>] [--force]`
// (FR-A6). It prints a generated temporary password once; there is no password flag, so no password lands in
// shell history. The Root Admin changes it at first sign-in (FR-A8).
// It returns the process exit code: 0 done, 1 failed, 2 bad arguments.
func runCreateRootAdmin(args []string, getenv func(string) string) int {
	fs := flag.NewFlagSet("create-root-admin", flag.ContinueOnError)
	username := fs.String("username", "", "username the Root Admin signs in with (required)")
	name := fs.String("name", "", "display name (default: the username)")
	force := fs.Bool("force", false, "add a Root Admin even if one exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	user := auth.NormalizeUsername(*username)
	if auth.UsernameProblem(user) != "" {
		fmt.Fprintln(os.Stderr, "create-root-admin: --username must be 3 to 64 characters from a-z 0-9 . _ - @")
		return 2
	}

	url := getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "create-root-admin: missing environment variable DATABASE_URL")
		return 1
	}
	db, err := models.Open(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create-root-admin:", err)
		return 1
	}
	password, err := createRootAdmin(db, user, *name, *force)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create-root-admin:", err)
		return 1
	}
	fmt.Println("Root Admin created:", user)
	fmt.Println("Temporary password (shown once; change it at first sign-in):", password)
	return 0
}

// createRootAdmin adds an active Root Admin with a generated temporary password and audits staff.created with
// actor "system" in one transaction. It returns the password; only its hash is stored (FR-A7).
func createRootAdmin(db *gorm.DB, username, name string, force bool) (string, error) {
	if name == "" {
		name = username
	}
	password := rand.Text() // 26 characters, 130 random bits
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		// Two installs running at once must not both see "no Root Admin yet".
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('create-root-admin'))").Error; err != nil {
			return fmt.Errorf("lock: %w", err)
		}
		var role models.Role
		if err := tx.Where("name = ?", "Root Admin").First(&role).Error; err != nil {
			return fmt.Errorf("find Root Admin role: %w", err)
		}
		var active int64
		if err := tx.Model(&models.Staff{}).Where("role_id = ? AND is_active", role.ID).Count(&active).Error; err != nil {
			return fmt.Errorf("count Root Admins: %w", err)
		}
		if active > 0 && !force {
			return errRootAdminExists
		}
		var taken int64
		if err := tx.Model(&models.Staff{}).Where("username = ?", username).Count(&taken).Error; err != nil {
			return fmt.Errorf("check username: %w", err)
		}
		if taken > 0 {
			return errors.New("a staff member with that username already exists")
		}
		st := models.Staff{Name: name, Username: username, PasswordHash: hash, MustChangePassword: true,
			PasswordChangedAt: time.Now(), RoleID: role.ID, Language: "en", IsActive: true}
		if err := tx.Create(&st).Error; err != nil {
			return fmt.Errorf("create staff: %w", err)
		}
		return audit.Record(tx, audit.System, "staff.created", audit.Change{Target: username, To: role.Name})
	})
	if err != nil {
		return "", err
	}
	return password, nil
}
