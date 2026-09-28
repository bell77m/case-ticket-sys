# Username and Password Sign-in Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Entra ID SSO with username and password sign-in for IT staff, with temporary passwords, forced change, Root Admin resets and a Redis login rate limit.

**Architecture:** Passwords are hashed with the Go standard library (`crypto/pbkdf2`, SHA-256). `staff` gains `username`, `password_hash`, `must_change_password` and `password_changed_at`; `email` becomes optional. A session remembers the `password_changed_at` it began with, so a password change or reset ends older sessions. `requireSession` covers `me` and the password change; `requireStaff` also refuses staff who still hold a temporary password. All OIDC code, the dev IdP and its dependencies go away.

**Tech Stack:** Go 1.27 (`net/http`, `crypto/pbkdf2`, GORM, go-redis v9), PostgreSQL 16 (goose), Redis 7, SvelteKit (Svelte 5, Paraglide), Playwright.

**Spec:** docs/REQUIREMENTS.md (FR-R1, FR-A1 to FR-A11, NFR-9, audit table) and docs/PLAN.md task **T2.15**. Read both before starting.

## Global Constraints

- No new Go or npm dependencies (CLAUDE.md "Stdlib first"). Hashing uses `crypto/pbkdf2` and `crypto/sha256`.
- Hash format: `pbkdf2-sha256$<iterations>$<salt>$<key>`, base64 without padding (`base64.RawStdEncoding`), 600,000 iterations, 16-byte salt, 32-byte key.
- Passwords: 12 to 128 characters (counted in runes), no composition rules.
- Usernames: stored trimmed and lower case, matching `^[a-z0-9._@-]{3,64}$`, unique.
- Login rate limit: 5 failures per username or 20 per IP within 15 minutes. After that, further attempts get 429 `auth.too_many_attempts`.
- Error codes: `auth.invalid`, `auth.too_many_attempts`, `auth.password_change_required`, `staff.username_taken`, `staff.email_taken`. Validation field codes: `required`, `invalid`, `too_short`, `too_long`, `same`, `wrong`.
- Audit actions: `login.success`, `login.failed` (target = username), `staff.password_changed`, `staff.password_reset` (never the password). Staff audit targets are usernames, not emails.
- Every Go test names its requirement ID. Every audited write happens in the same transaction as its change (`audit.Record(tx, ...)`).
- Every user-facing string is a Paraglide key in all four message files (en, zh-CN, my, th). English is the placeholder until the i18n-translator agent runs.
- UI uses DESIGN.md tokens only; no hard-coded colors, sizes or radii.
- Dev password for seeded staff `root`, `agent`, `viewer`: `dev-password`.
- This folder is **not a git repository**. Where a step would commit, it runs the tests instead as a checkpoint.

## Review Focus

1. **Everyone shares one client IP behind NGINX.** `clientIP` returns the proxy's address until T3.10, so the per-IP limit (20 failures in 15 minutes) could lock out every staff member at once. Expected: one person's typos never block anyone else. Task 3 adds a test that failures from one IP leave another IP free, and Task 8 records the T3.10 dependency in docs/PLAN.md.
2. **Username typed with capitals or spaces** ("  Somchai.K "). Expected: it signs in as `somchai.k`. Pinned by `TestLogin_Success_FRR1` in Task 3.
3. **Session cookie from before the upgrade** (Redis holds just a staff ID). Expected: 401 and a fresh sign-in, never a 500. Pinned by `TestSessionOldFormat_FRA10` in Task 3.
4. **Deep link while a temporary password is pending** (for example `/staff/admin/staff` right after the first sign-in). Expected: the browser lands on `/staff/password`, not an error page. Pinned by the e2e admin test in Task 7.
5. **Root Admin resets their own password.** Expected: their own session ends, and they can sign in with the temporary password and must then change it. There is no lockout. Pinned by `TestResetPassword_Self_FRA9` in Task 5.

---

## File Map

| File | Change | Responsibility |
| --- | --- | --- |
| `backend/internal/auth/credentials.go` | create | Hash and check passwords; username and password rules |
| `backend/internal/auth/credentials_test.go` | create | Unit tests, no DB |
| `backend/internal/auth/limit.go` | create | Failed sign-in counters in Redis (FR-A11) |
| `backend/internal/auth/session.go` | modify | Session carries a password stamp; OIDC pending-login code removed |
| `backend/internal/auth/session_test.go` | create | Session format test (Redis) |
| `backend/internal/auth/oidc.go`, `backend/internal/auth/oidcmock/` | delete | |
| `backend/cmd/dev-oidc/` | delete | |
| `backend/migrations/00003_staff_passwords.sql` | create | New staff columns |
| `backend/migrations/migrations_test.go` | modify | Username format test |
| `backend/internal/models/models.go` | modify | `Staff` fields; `Staff` refuses `json.Marshal` |
| `backend/seed/dev.sql` | modify | Dev staff with usernames and the dev password |
| `backend/cmd/ticket-app/rootadmin.go` (+ test) | modify | `--username`; prints a temporary password |
| `backend/cmd/ticket-app/main.go` | modify | No OIDC wiring |
| `backend/internal/config/config.go` (+ test) | modify | No OIDC settings |
| `backend/internal/api/auth.go` | rewrite | Login, logout, requireSession, requireStaff, me, changePassword |
| `backend/internal/api/api.go` | modify | Routes; no OIDC field |
| `backend/internal/api/staff.go` | modify | Create with username, password and optional email; reset password |
| `backend/internal/api/auth_test.go`, `password_test.go`, `staff_test.go`, `audit_coverage_test.go`, `helpers_test.go` | modify/create | Tests |
| `frontend/src/lib/api.ts` | modify | `Me`, `login`, `changePassword`, staff account calls |
| `frontend/src/routes/login/+page.svelte` | rewrite | Username and password form |
| `frontend/src/routes/staff/password/+page.svelte` | create | Change password page |
| `frontend/src/routes/staff/+layout.ts` | modify | Redirect to `/staff/password` while a temporary password is pending |
| `frontend/src/routes/+layout.svelte` | modify | Menu shows the username; "Change password" link |
| `frontend/src/routes/staff/admin/staff/+page.svelte` | modify | Username, optional email, temporary password, reset |
| `frontend/messages/{en,zh-CN,my,th}.json` | modify | Keys |
| `frontend/e2e/*.spec.ts`, `frontend/e2e/helpers.ts`, `frontend/playwright.config.ts` | modify | Sign-in helper and specs; no dev-oidc server |
| `Makefile`, `.env.example`, `CLAUDE.md`, `.claude/rules/frontend.md`, `docs/*` | modify | Commands and docs |

---

### Task 1: Password and username rules

**Files:**
- Create: `backend/internal/auth/credentials.go`
- Test: `backend/internal/auth/credentials_test.go`

**Interfaces:**
- Produces:
  - `auth.HashPassword(password string) (string, error)`
  - `auth.CheckPassword(encoded, password string) bool`
  - `auth.PasswordProblem(p string) string` returns `""`, `"required"`, `"too_short"` or `"too_long"`.
  - `auth.NormalizeUsername(s string) string`
  - `auth.UsernameProblem(u string) string` returns `""`, `"required"` or `"invalid"`.

- [ ] **Step 1: Write the failing tests**

`backend/internal/auth/credentials_test.go`:

```go
package auth

import (
	"strings"
	"testing"
	"time"
)

// FR-A7: a hash verifies its own password only, two hashes of one password differ (random salt), and the
// format names its algorithm and work factor so both can change later.
func TestHashPassword_FRA7(t *testing.T) {
	for _, pw := range []string{"correct horse battery", "รหัสผ่านภาษาไทยยาว", "မြန်မာစကားဝှက်ရှည်"} {
		h1, err := HashPassword(pw)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(h1, "pbkdf2-sha256$600000$") || strings.Count(h1, "$") != 3 {
			t.Errorf("hash %q, want pbkdf2-sha256$600000$<salt>$<key>", h1)
		}
		h2, _ := HashPassword(pw)
		if h1 == h2 {
			t.Errorf("two hashes of %q are equal; salt is not random", pw)
		}
		if !CheckPassword(h1, pw) {
			t.Errorf("CheckPassword(own hash, %q) = false", pw)
		}
		if CheckPassword(h1, pw+"x") || CheckPassword(h1, strings.ToUpper(pw)) {
			t.Errorf("CheckPassword accepted a different password for %q", pw)
		}
	}
}

// FR-A2: an account without a password (empty hash) or with a broken hash never matches.
func TestCheckPassword_NoHash_FRA2(t *testing.T) {
	for _, enc := range []string{"", "plain", "pbkdf2-sha256$x$y$z", "bcrypt$1$2$3", "pbkdf2-sha256$600000$!!$!!"} {
		if CheckPassword(enc, "") || CheckPassword(enc, "anything at all") {
			t.Errorf("CheckPassword(%q, ...) = true, want false", enc)
		}
	}
}

// FR-A2: checking against no hash costs about as much as a real check, so response time does not tell
// which usernames exist.
func TestCheckPassword_SameCost_FRA2(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	CheckPassword("", "warm up the dummy hash")
	start := time.Now()
	CheckPassword(h, "wrong password!")
	real := time.Since(start)
	start = time.Now()
	CheckPassword("", "wrong password!")
	none := time.Since(start)
	if none < real/2 {
		t.Errorf("check without a hash took %v, real check %v; want about the same", none, real)
	}
}

// FR-A7: 12 to 128 characters, counted as characters (Thai and Burmese letters are several bytes each).
func TestPasswordProblem_FRA7(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "required"},
		{strings.Repeat("a", 11), "too_short"},
		{strings.Repeat("a", 12), ""},
		{strings.Repeat("ก", 12), ""},
		{strings.Repeat("a", 128), ""},
		{strings.Repeat("a", 129), "too_long"},
		{"            ", ""}, // spaces are characters too; no composition rules (NIST 800-63B)
	}
	for _, tt := range tests {
		if got := PasswordProblem(tt.in); got != tt.want {
			t.Errorf("PasswordProblem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// FR-A1: usernames are 3 to 64 characters from a-z 0-9 . _ - @, compared in lower case.
func TestUsernameProblem_FRA1(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "required"},
		{"ab", "invalid"},
		{"abc", ""},
		{"somchai.k", ""},
		{"a_b-c@d.e", ""},
		{strings.Repeat("a", 64), ""},
		{strings.Repeat("a", 65), "invalid"},
		{"Somchai", "invalid"}, // callers normalize first
		{"som chai", "invalid"},
		{"som+chai", "invalid"},
		{"ซมชาย", "invalid"},
	}
	for _, tt := range tests {
		if got := UsernameProblem(tt.in); got != tt.want {
			t.Errorf("UsernameProblem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := NormalizeUsername("  SomChai.K "); got != "somchai.k" {
		t.Errorf("NormalizeUsername = %q, want somchai.k", got)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `cd backend && go test ./internal/auth -run 'Password|Username' -v`
Expected: build failure, `undefined: HashPassword` (and the others).

- [ ] **Step 3: Write the implementation**

`backend/internal/auth/credentials.go`:

```go
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// passwordIter is the PBKDF2-HMAC-SHA256 work factor (OWASP 2023). Each hash stores its own count, so it can
// rise later without breaking existing passwords.
const passwordIter = 600_000

// FR-A7: length limits in characters; no composition rules (NIST 800-63B).
const (
	passwordMin = 12
	passwordMax = 128
)

var b64 = base64.RawStdEncoding

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<key>" with a 16-byte random salt (FR-A7).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt) // never fails: crypto/rand crashes the program instead (Go 1.24+)
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIter, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// dummyHash stands in for an unknown account or one without a password, so checking it costs the same.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword(rand.Text())
	if err != nil {
		panic(err)
	}
	return h
})

// CheckPassword reports whether password matches encoded. An empty or foreign encoded value never matches but
// still costs one full hash, so response time does not tell which usernames exist (FR-A2).
func CheckPassword(encoded, password string) bool {
	match := true
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		match = false
		parts = strings.Split(dummyHash(), "$")
	}
	iter, err1 := strconv.Atoi(parts[1])
	salt, err2 := b64.DecodeString(parts[2])
	want, err3 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || iter < 1 || len(want) == 0 {
		return false // a corrupt row; only HashPassword writes this column
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && match && subtle.ConstantTimeCompare(got, want) == 1
}

// PasswordProblem returns "required", "too_short" or "too_long" (FR-A7), or "" when the password is fine.
func PasswordProblem(p string) string {
	switch n := utf8.RuneCountInString(p); {
	case n == 0:
		return "required"
	case n < passwordMin:
		return "too_short"
	case n > passwordMax:
		return "too_long"
	}
	return ""
}

var usernameRE = regexp.MustCompile(`^[a-z0-9._@-]{3,64}$`)

// NormalizeUsername is the form usernames are stored and compared in: trimmed, lower case.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// UsernameProblem returns "required" or "invalid" for a normalized username, or "" when it is fine.
func UsernameProblem(u string) string {
	switch {
	case u == "":
		return "required"
	case !usernameRE.MatchString(u):
		return "invalid"
	}
	return ""
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && go test ./internal/auth -run 'Password|Username' -v`
Expected: all five tests PASS.

- [ ] **Step 5: Checkpoint**

Run: `cd backend && go vet ./internal/auth && gofmt -l internal/auth`
Expected: no output.

---

### Task 2: Staff schema, model, seed, create-root-admin and staff creation

**Files:**
- Create: `backend/migrations/00003_staff_passwords.sql`
- Modify: `backend/migrations/migrations_test.go` (append a test)
- Modify: `backend/internal/models/models.go:73-83` (`Staff`)
- Modify: `backend/seed/dev.sql:20-30`
- Modify: `backend/cmd/ticket-app/rootadmin.go`, `backend/cmd/ticket-app/rootadmin_test.go`
- Modify: `backend/internal/api/staff.go` (`staffAccount`, `accountQuery`, errors, `createStaff`, audit targets in `updateStaff`, `writeStaffError`)
- Modify: `backend/internal/api/auth.go` (`meOutput`, `me` only)
- Modify: `backend/internal/api/auth_test.go` (`newStaff`, email dereferences), `staff_test.go`, `audit_coverage_test.go`

**Interfaces:**
- Consumes: Task 1 (`auth.HashPassword`, `auth.CheckPassword`, `auth.PasswordProblem`, `auth.NormalizeUsername`, `auth.UsernameProblem`)
- Produces:
  - `models.Staff{Username string; Email *string; PasswordHash string; MustChangePassword bool; PasswordChangedAt time.Time}`
  - `staffAccount{Username string "username"; Email string "email"}`, where `""` means no email.
  - `meOutput{Username string "username"; MustChangePassword bool "must_change_password"}`, without `email`.
  - `errUsernameTaken`, which maps to 409 `staff.username_taken`.
  - Test constants `testPassword` and `testHash()`. `newStaff` gives every test staff member `testPassword`.
  - `createRootAdmin(db, username, email, name string, force bool) (password string, err error)`

- [ ] **Step 1: Write the failing migration test**

Append to `backend/migrations/migrations_test.go`:

```go
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
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `make test-go PKG=./migrations RUN=StaffUsernameFormat`
Expected: FAIL, `column "username" of relation "staff" does not exist`.

- [ ] **Step 3: Write the migration**

`backend/migrations/00003_staff_passwords.sql`:

```sql
-- Staff sign in with a username and password instead of SSO (T2.15: FR-R1, FR-A1, FR-A7 to FR-A10).
-- Email stays, optional, for notifications only (T2.12).

-- +goose Up
ALTER TABLE staff
    ADD COLUMN username             text,
    ADD COLUMN password_hash        text        NOT NULL DEFAULT '', -- '' = no password yet: sign-in always fails
    ADD COLUMN must_change_password boolean     NOT NULL DEFAULT true,
    ADD COLUMN password_changed_at  timestamptz NOT NULL DEFAULT now(),
    ALTER COLUMN email DROP NOT NULL;
-- Existing accounts get their email as username and no password; Root Admin resets them (FR-A9).
-- An email with other characters (such as '+') fails the CHECK below: fix that row by hand, then migrate again.
UPDATE staff SET username = lower(email);
ALTER TABLE staff
    ALTER COLUMN username SET NOT NULL,
    ADD CONSTRAINT staff_username_format CHECK (username ~ '^[a-z0-9._@-]{3,64}$');
CREATE UNIQUE INDEX staff_username_key ON staff (username);

-- +goose Down
DROP INDEX staff_username_key;
UPDATE staff SET email = username || '@invalid' WHERE email IS NULL;
ALTER TABLE staff
    DROP COLUMN username,
    DROP COLUMN password_hash,
    DROP COLUMN must_change_password,
    DROP COLUMN password_changed_at,
    ALTER COLUMN email SET NOT NULL;
```

Run: `make migrate`, then `make migrate-down`, then `make migrate`.
Expected: all three succeed. The round trip proves Down works.

- [ ] **Step 4: Run the migration test and confirm it passes**

Run: `make test-go PKG=./migrations RUN=StaffUsernameFormat`
Expected: PASS.

- [ ] **Step 5: Update the model**

In `backend/internal/models/models.go`, replace the `Staff` struct and add its JSON guard below `Comment`'s:

```go
type Staff struct {
	ID                 int64
	Name               string
	Username           string  // sign-in name, lower case (FR-A1)
	Email              *string // optional, for notifications only (T2.12); never used to sign in
	PasswordHash       string  // "" until a password is set; see auth.HashPassword (FR-A7)
	MustChangePassword bool    // a temporary password must be replaced first (FR-A8)
	PasswordChangedAt  time.Time
	RoleID             int64
	Language           string
	IsActive           bool
	CreatedAt          time.Time
}
```

```go
// MarshalJSON always fails; see errUseResponseType. Staff holds the password hash.
func (Staff) MarshalJSON() ([]byte, error) { return nil, errUseResponseType }
```

Run: `cd backend && grep -rn "models.Staff" internal/api | grep -v _test`
Expected: no line passes a `models.Staff` value to `writeJSON` or `json.Marshal`. The call sites map it to `meOutput`, `staffAccount` or `namedItem`. If one does, map it to a response struct before going on.

- [ ] **Step 6: Rewrite the create-root-admin test (failing)**

Replace `backend/cmd/ticket-app/rootadmin_test.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

// FR-A6, FR-A8: the first run creates the Root Admin with a printed temporary password that signs in and must be
// changed; a second run refuses unless --force; a taken username or email is refused.
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
	tx := db.Begin()
	defer tx.Rollback()
	if err := tx.Exec(`UPDATE staff SET is_active = false WHERE role_id = (SELECT id FROM roles WHERE name = 'Root Admin')`).Error; err != nil {
		t.Fatal(err)
	}
	user := func(n int) string { return fmt.Sprintf("root%d-%d", n, time.Now().UnixNano()) }

	first := user(1)
	password, err := createRootAdmin(tx, first, "", "", false)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	var st models.Staff
	if err := tx.Where("username = ? AND is_active", first).First(&st).Error; err != nil {
		t.Fatalf("root admin not created: %v", err)
	}
	var role string
	tx.Raw("SELECT name FROM roles WHERE id = ?", st.RoleID).Scan(&role)
	if role != "Root Admin" || st.Name != first || st.Email != nil {
		t.Errorf("created role %q name %q email %v, want Root Admin named after the username, no email", role, st.Name, st.Email)
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

	if _, err := createRootAdmin(tx, user(2), "", "", false); !errors.Is(err, errRootAdminExists) {
		t.Errorf("second run error = %v, want errRootAdminExists", err)
	}
	email := fmt.Sprintf("second%d@test.local", time.Now().UnixNano())
	if _, err := createRootAdmin(tx, user(3), email, "Second Admin", true); err != nil {
		t.Errorf("second run with --force: %v", err)
	}
	if _, err := createRootAdmin(tx, first, "", "", true); err == nil {
		t.Errorf("existing username accepted twice")
	}
	if _, err := createRootAdmin(tx, user(4), email, "", true); err == nil {
		t.Errorf("existing email accepted twice")
	}
}

func TestRunCreateRootAdmin_BadArgs_FRA6(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--email", "a@b.c"},
		{"--username", "ab"},
		{"--username", "Has Space"},
		{"--username", "okname", "--email", "not-an-email"},
		{"--username", "okname", "--email", "Name <a@b.c>"},
	} {
		if code := runCreateRootAdmin(args, func(string) string { return "" }); code != 2 {
			t.Errorf("args %q exit = %d, want 2", args, code)
		}
	}
}
```

Run: `make test-go PKG=./cmd/ticket-app RUN=RootAdmin`
Expected: build failure (`createRootAdmin` returns one value; too many arguments).

- [ ] **Step 7: Rewrite create-root-admin**

Replace `backend/cmd/ticket-app/rootadmin.go`:

```go
package main

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

var errRootAdminExists = errors.New("an active Root Admin already exists; use --force to add another")

// runCreateRootAdmin is `ticket-app create-root-admin --username <u> [--name <name>] [--email <email>] [--force]`
// (FR-A6). It prints a generated temporary password once; there is no password flag, so no password lands in
// shell history. The Root Admin changes it at first sign-in (FR-A8).
// It returns the process exit code: 0 done, 1 failed, 2 bad arguments.
func runCreateRootAdmin(args []string, getenv func(string) string) int {
	fs := flag.NewFlagSet("create-root-admin", flag.ContinueOnError)
	username := fs.String("username", "", "username the Root Admin signs in with (required)")
	name := fs.String("name", "", "display name (default: the username)")
	email := fs.String("email", "", "work email for notifications (optional)")
	force := fs.Bool("force", false, "add a Root Admin even if one exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	user := auth.NormalizeUsername(*username)
	if auth.UsernameProblem(user) != "" {
		fmt.Fprintln(os.Stderr, "create-root-admin: --username must be 3 to 64 characters from a-z 0-9 . _ - @")
		return 2
	}
	addr := strings.TrimSpace(*email)
	if addr != "" {
		if a, err := mail.ParseAddress(addr); err != nil || a.Address != addr {
			fmt.Fprintln(os.Stderr, "create-root-admin: --email must be a plain email address")
			return 2
		}
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
	password, err := createRootAdmin(db, user, strings.ToLower(addr), *name, *force)
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
func createRootAdmin(db *gorm.DB, username, email, name string, force bool) (string, error) {
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
		if err := tx.Model(&models.Staff{}).Where("username = ? OR (? <> '' AND lower(email) = ?)", username, email, email).
			Count(&taken).Error; err != nil {
			return fmt.Errorf("check username: %w", err)
		}
		if taken > 0 {
			return errors.New("a staff member with that username or email already exists")
		}
		st := models.Staff{Name: name, Username: username, PasswordHash: hash, MustChangePassword: true,
			PasswordChangedAt: time.Now(), RoleID: role.ID, Language: "en", IsActive: true}
		if email != "" {
			st.Email = &email
		}
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
```

- [ ] **Step 8: Update the test helpers and staff tests (failing)**

In `backend/internal/api/auth_test.go`, replace `newStaff` and add the password fixtures above it (add `"sync"` to the imports):

```go
// testPassword is every test staff member's password; testHash is its hash, computed once (hashing is slow on purpose).
const testPassword = "test-password-123"

var testHash = sync.OnceValue(func() string {
	h, err := auth.HashPassword(testPassword)
	if err != nil {
		panic(err)
	}
	return h
})

// newStaff adds a staff member with a unique username and email and password testPassword. Cleanup deletes it,
// or only deactivates it once audit_log points at it (FR-L2 forbids deleting those audit rows).
func (e *testEnv) newStaff(role string, active bool) models.Staff {
	e.t.Helper()
	n := time.Now().UnixNano()
	email := fmt.Sprintf("t%d@Test.Local", n)
	st := models.Staff{
		Name: "Test Staff", Username: fmt.Sprintf("t%d", n), Email: &email, PasswordHash: testHash(),
		// Microseconds, like the DB column, so a session stamped from this struct matches the row (FR-A10).
		PasswordChangedAt: time.Now().Truncate(time.Microsecond), IsActive: active, Language: "en",
	}
	if err := e.db.Raw("SELECT id FROM roles WHERE name = ?", role).Scan(&st.RoleID).Error; err != nil || st.RoleID == 0 {
		e.t.Fatalf("role %q: %v", role, err)
	}
	if err := e.db.Create(&st).Error; err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { e.dropStaff(st.ID) })
	return st
}
```

The SSO tests in `auth_test.go` stay until Task 3. Keep them compiling: replace `e.login(idp, st.Email)` with `e.login(idp, *st.Email)`, `strings.ToUpper(st.Email)` with `strings.ToUpper(*st.Email)` and `strings.ToLower(st.Email)` with `strings.ToLower(*st.Email)`.

In `backend/internal/api/audit_coverage_test.go`:

- `"POST /api/staff/accounts"` case: build the body as

  ```go
  username := fmt.Sprintf("new%d", time.Now().UnixNano())
  body := fmt.Sprintf(`{"name": "New Agent", "username": %q, "role_id": %d, "password": %q}`, username, e.roleID("Agent"), testPassword)
  ```

  and return `[]wantAudit{{action: "staff.created", target: username}}`.
- `"PATCH /api/staff/accounts/{id}"` case: targets become `st.Username`.
- `"GET /api/auth/callback"` case: `e.login(idp, *st.Email)` and `strings.ToLower(*st.Email)`.

In `backend/internal/api/staff_test.go`:

- `TestStaffList_FRA1`: the check becomes `a.Username != off.Username || a.Email != *off.Email || ...`.
- `TestStaffRole_FRA4`: `e.staffAudits(st.Email, since)` becomes `e.staffAudits(st.Username, since)`.
- `TestStaffRootOnly_FRA3`: the create body becomes `fmt.Sprintf(`{"name": "X", "username": "x%d", "role_id": %d, "password": %q}`, time.Now().UnixNano(), e.roleID("Root Admin"), testPassword)`.
- `TestStaffSignIn_FRA2` (SSO until Task 3): add `"username"` and `"password"` to its body, and keep `"email"`:

  ```go
  body := fmt.Sprintf(`{"name": "New Agent", "username": "new%d", "email": %q, "role_id": %d, "password": %q}`,
  	time.Now().UnixNano(), email, e.roleID("Agent"), testPassword)
  rec := e.sendJSON(http.MethodPost, "/api/staff/accounts", root, body)
  ```

- Replace `TestStaffCreate_FRA1` (add `"ticket-app/internal/auth"` to the imports):

```go
// FR-A1: only Root Admin creates staff accounts (name, username, optional email, temporary password, role). Input
// is checked per field; username and email are stored lower case and unique ignoring case; the password is stored
// only as a hash and must be changed at first sign-in (FR-A8); staff.created is audited.
func TestStaffCreate_FRA1(t *testing.T) {
	e, _ := newAuthEnv(t)
	rootSt := e.newStaff("Root Admin", true)
	root, admin, lead := e.session(rootSt), e.session(e.newStaff("Admin", true)), e.session(e.newStaff("Team Lead", true))
	taken := e.newStaff("Agent", true)
	agent := e.roleID("Agent")
	user := func() string { return fmt.Sprintf("New.%d", time.Now().UnixNano()) }
	mail := func() string { return fmt.Sprintf("New%d@Test.Local", time.Now().UnixNano()) }
	in := func(name, username, email string, role int64, password string) map[string]any {
		return map[string]any{"name": name, "username": username, "email": email, "role_id": role, "password": password}
	}
	pw := "temporary-pass-1"

	tests := []struct {
		name   string
		c      *http.Cookie
		in     map[string]any
		code   int
		err    string
		fields map[string]string
	}{
		{"Root Admin creates", root, in(" New Agent ", " "+user()+" ", mail(), agent, pw), 201, "", nil},
		{"Root Admin creates without email", root, in("New Agent", user(), "", agent, pw), 201, "", nil},
		{"Admin refused", admin, in("New Agent", user(), "", agent, pw), 403, "auth.forbidden", nil},
		{"Team Lead refused", lead, in("New Agent", user(), "", agent, pw), 403, "auth.forbidden", nil},
		{"username with space", root, in("X", "new agent", "", agent, pw), 400, "validation", map[string]string{"username": "invalid"}},
		{"username too short", root, in("X", "ab", "", agent, pw), 400, "validation", map[string]string{"username": "invalid"}},
		{"username missing", root, in("X", "  ", "", agent, pw), 400, "validation", map[string]string{"username": "required"}},
		{"username taken in other case", root, in("X", strings.ToUpper(taken.Username), "", agent, pw), 409, "staff.username_taken", nil},
		{"email not an address", root, in("X", user(), "not-an-email", agent, pw), 400, "validation", map[string]string{"email": "invalid"}},
		{"email with display name", root, in("X", user(), "Bob <bob@test.local>", agent, pw), 400, "validation", map[string]string{"email": "invalid"}},
		{"email with format character", root, in("X", user(), "bob"+string(rune(0x202E))+"@test.local", agent, pw), 400, "validation", map[string]string{"email": "invalid"}},
		{"email taken in other case", root, in("X", user(), strings.ToUpper(*taken.Email), agent, pw), 409, "staff.email_taken", nil},
		{"password too short", root, in("X", user(), "", agent, "short"), 400, "validation", map[string]string{"password": "too_short"}},
		{"password missing", root, in("X", user(), "", agent, ""), 400, "validation", map[string]string{"password": "required"}},
		{"role unknown", root, in("X", user(), "", 999999999, pw), 400, "validation", map[string]string{"role_id": "not_found"}},
		{"name blank", root, in("  ", user(), "", agent, pw), 400, "validation", map[string]string{"name": "required"}},
		{"name too long", root, in(strings.Repeat("a", 101), user(), "", agent, pw), 400, "validation", map[string]string{"name": "too_long"}},
		{"all fields bad", root, in("", "", "x", 0, ""), 400, "validation",
			map[string]string{"name": "required", "username": "required", "email": "invalid", "password": "required", "role_id": "not_found"}},
		{"permissions field", root, map[string]any{"name": "X", "username": user(), "role_id": agent, "password": pw, "permissions": []string{rbac.RoleManage}}, 400, "invalid_body", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			username := auth.NormalizeUsername(tt.in["username"].(string))
			count := func() (n int64) {
				e.db.Model(&models.Staff{}).Where("username = ?", username).Count(&n)
				return n
			}
			before, since := count(), e.lastAuditID()
			body, _ := json.Marshal(tt.in)
			rec := e.sendJSON(http.MethodPost, "/api/staff/accounts", tt.c, string(body))
			code, fields := errorBody(rec)
			if rec.Code != tt.code || code != tt.err || !maps.Equal(fields, tt.fields) {
				t.Fatalf("POST = %d %s, want %d %s %v", rec.Code, rec.Body, tt.code, tt.err, tt.fields)
			}
			if tt.code != http.StatusCreated {
				if n := count(); n != before {
					t.Errorf("refused request changed staff with that username: %d rows, was %d", n, before)
				}
				return
			}
			var got staffAccount
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			e.t.Cleanup(func() { e.dropStaff(got.ID) })
			wantEmail := strings.ToLower(tt.in["email"].(string))
			if got.ID == 0 || got.Name != "New Agent" || got.Username != username || got.Email != wantEmail ||
				got.Role != (namedItem{agent, "Agent"}) || !got.IsActive || got.CreatedAt.IsZero() {
				t.Errorf("created = %+v", got)
			}
			if row := e.staffRow(got.ID); !row.MustChangePassword || !auth.CheckPassword(row.PasswordHash, pw) || strings.Contains(row.PasswordHash, pw) {
				t.Errorf("stored password: must change %v, hash matches %v", row.MustChangePassword, auth.CheckPassword(row.PasswordHash, pw))
			}
			if a := e.staffAudits(username, since); !slices.Equal(a, []string{"staff.created >Agent"}) {
				t.Errorf("audits = %v, want staff.created to Agent", a)
			}
			if actor, id, _ := e.auditRow("staff.created", username); actor != "staff" || id == nil || *id != rootSt.ID {
				t.Errorf("staff.created actor = %s %v, want the Root Admin", actor, id)
			}
		})
	}
}
```

Run: `make test-go PKG=./internal/api RUN='Staff|AuditCoverage|Login|Logout|Session'`
Expected: build failure (`got.Username` undefined on `staffAccount`) until Step 9 is done; then PASS.

- [ ] **Step 9: Update the API to the new model (these compile fixes are also spec changes)**

In `backend/internal/api/auth.go`, replace `meOutput` and `me`'s first lines:

```go
type meOutput struct {
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	Username           string   `json:"username"`
	Role               string   `json:"role"`
	Language           string   `json:"language"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"must_change_password"` // the frontend sends staff to /staff/password (FR-A8)
}
```

```go
	out := meOutput{ID: st.ID, Name: st.Name, Username: st.Username, Language: st.Language,
		MustChangePassword: st.MustChangePassword, Permissions: []string{}}
```

In `backend/internal/api/staff.go`:

1. Replace the header comment, `staffAccount` and `accountQuery`:

```go
// Staff accounts (T2.08, T2.15): list, create (Root Admin only), set role, deactivate, reset password (FR-A1 to FR-A9).

type staffAccount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Email     string    `json:"email"` // "" when none
	Role      namedItem `json:"role" gorm:"embedded;embeddedPrefix:role_"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

const accountQuery = `SELECT s.id, s.name, s.username, coalesce(s.email, '') AS email, r.id AS role_id, r.name AS role_name,
	s.is_active, s.created_at FROM staff s JOIN roles r ON r.id = s.role_id`
```

2. Add `errUsernameTaken = errors.New("username taken")` to the `var (...)` block.

3. Replace `createStaff`:

```go
// POST /api/staff/accounts {"name", "username", "email"?, "role_id", "password"} — Root Admin adds a staff member
// (FR-A1). The password is temporary: it must be changed at first sign-in (FR-A8). Email is optional, for
// notifications only.
func (s *Server) createStaff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Email    string `json:"email"`
		RoleID   int64  `json:"role_id"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	db := s.DB.WithContext(r.Context())
	name, username, email := strings.TrimSpace(in.Name), auth.NormalizeUsername(in.Username), strings.TrimSpace(in.Email)
	fields := map[string]string{}
	if p := textProblem("name", name, 1, 100); p != "" {
		fields["name"] = p
	}
	if p := auth.UsernameProblem(username); p != "" {
		fields["username"] = p
	}
	// Optional. When given: a bare address only: no display name, no invisible or format characters, at most 254 bytes.
	if email != "" {
		if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 || strings.ContainsFunc(email, badRune("employee_id")) {
			fields["email"] = "invalid"
		}
	}
	if p := auth.PasswordProblem(in.Password); p != "" {
		fields["password"] = p
	}
	role, err := findRole(db, in.RoleID)
	if errors.Is(err, errRoleNotFound) {
		fields["role_id"] = "not_found"
	} else if err != nil {
		writeStaffError(w, err, "read role")
		return
	}
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
		return
	}
	hash, err := auth.HashPassword(in.Password) // before the transaction: hashing takes a moment on purpose
	if err != nil {
		writeStaffError(w, err, "hash password")
		return
	}

	email = strings.ToLower(email) // the unique index is on lower(email)
	var out staffAccount
	err = db.Transaction(func(tx *gorm.DB) error {
		var id int64
		if err := tx.Raw(`INSERT INTO staff (name, username, email, role_id, password_hash, must_change_password)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, true) ON CONFLICT DO NOTHING RETURNING id`,
			name, username, email, role.ID, hash).Scan(&id).Error; err != nil {
			return err
		}
		if id == 0 { // username or email taken; say which
			var taken bool
			if err := tx.Raw("SELECT EXISTS (SELECT 1 FROM staff WHERE username = ?)", username).Scan(&taken).Error; err != nil {
				return err
			}
			if taken {
				return errUsernameTaken
			}
			return errEmailTaken
		}
		if err := audit.Record(tx, audit.Staff(currentStaff(r).ID), "staff.created", audit.Change{Target: username, To: role.Name, IP: clientIP(r)}); err != nil {
			return err
		}
		return tx.Raw(accountQuery+" WHERE s.id = ?", id).Scan(&out).Error
	})
	if err != nil {
		writeStaffError(w, err, "create staff")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
```

4. In `updateStaff`, change both `audit.Change{Target: st.Email, ...}` to `Target: st.Username`.

5. In `writeStaffError`, add above the `errEmailTaken` case:

```go
	case errors.Is(err, errUsernameTaken):
		writeError(w, http.StatusConflict, "staff.username_taken")
```

6. Add `"ticket-app/internal/auth"` to the imports.

- [ ] **Step 10: Seed the dev staff**

Generate the dev password hash (Node's PBKDF2 matches Go's; padding is stripped to match `RawStdEncoding`):

```bash
node -e "const c=require('crypto');const s=c.randomBytes(16);const k=c.pbkdf2Sync('dev-password',s,600000,32,'sha256');const b=x=>x.toString('base64').replace(/=+$/,'');console.log('pbkdf2-sha256\$600000\$'+b(s)+'\$'+b(k))"
```

Replace the staff block of `backend/seed/dev.sql` (lines 20–30). Use the printed value for `<HASH>`:

```sql
-- Dev staff: sign in at /login as root, agent or viewer with password `dev-password` (T2.15). One per default role.
-- Re-running resets their usernames and passwords.
INSERT INTO staff (name, username, email, role_id, password_hash, must_change_password)
SELECT s.name, s.username, s.email, r.id,
       '<HASH>', -- gitleaks:allow (dev-only hash of "dev-password")
       false
FROM (VALUES
    ('Dev Root Admin', 'root', 'root@dev.test', 'Root Admin'),
    ('Dev Agent', 'agent', 'agent@dev.test', 'Agent'),
    ('Dev Viewer', 'viewer', 'viewer@dev.test', 'Viewer')
) AS s (name, username, email, role)
JOIN roles r ON r.name = s.role
ON CONFLICT ((lower(email))) DO UPDATE
    SET username = EXCLUDED.username, password_hash = EXCLUDED.password_hash,
        must_change_password = false, password_changed_at = now();
```

Run: `make seed`, twice.
Expected: no error either time.

The hash is proven in Task 7: every e2e test signs in as `root`, `agent` or `viewer` with `dev-password`, so a wrong hash fails the first e2e run at sign-in.

- [ ] **Step 11: Run the package tests and confirm they pass**

Run: `make test-go PKG=./cmd/ticket-app` then `make test-go PKG=./internal/api` then `make test-go PKG=./migrations`
Expected: all PASS.

- [ ] **Step 12: Checkpoint**

Run: `make test`
Expected: PASS (Go + i18n).

---

### Task 3: Password sign-in, sessions tied to the password, rate limit, temporary-password gate

**Files:**
- Create: `backend/internal/auth/limit.go`, `backend/internal/auth/session_test.go`
- Modify: `backend/internal/auth/session.go` (`Create`, `Get`, `Session`)
- Rewrite: `backend/internal/api/auth.go` (everything except `require`, `currentStaff`, `meOutput` and `me`)
- Modify: `backend/internal/api/api.go` (auth routes)
- Modify: `backend/internal/api/helpers_test.go` (`ip` field)
- Rewrite: the test part of `backend/internal/api/auth_test.go`
- Modify: `backend/internal/api/audit_coverage_test.go`, `staff_test.go`, and every `*_test.go` that calls `newAuthEnv`

**Interfaces:**
- Consumes: Task 1 (`CheckPassword`, `NormalizeUsername`), Task 2 (`models.Staff`, `testPassword`, `newStaff`)
- Produces:
  - `auth.Session{StaffID, Stamp int64}`, where `Stamp` is `password_changed_at` in Unix microseconds.
  - `(*auth.Sessions).Create(ctx, staffID, stamp int64) (string, error)` and `(*auth.Sessions).Get(ctx, id) (Session, error)`.
  - `(*auth.Sessions).LoginBlocked(ctx, username, ip string) (bool, error)`, `LoginFailed(ctx, username, ip string) error` and `LoginSucceeded(ctx, username string) error`.
  - `(*api.Server).requireSession` and `requireStaff`; `sessionCookieFor(value string, maxAge int) *http.Cookie`; `loginInput`.
  - Test helpers: `newAuthEnv(t) *testEnv` (one return value), `e.login(username, password)`, `e.ip`, `randomIP()`.

- [ ] **Step 1: Write the failing session test**

`backend/internal/auth/session_test.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// FR-A10: a session carries the password stamp it began with. A session saved before T2.15 holds only a staff
// ID; it reads as not found, so that browser signs in again instead of failing.
func TestSessionOldFormat_FRA10(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, s := context.Background(), &Sessions{Redis: rdb}

	old := rand.Text()
	if err := rdb.Set(ctx, sessionKey(old), 42, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Delete(ctx, old) })
	if _, err := s.Get(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old-format session: err = %v, want ErrNotFound", err)
	}

	id, err := s.Create(ctx, 42, 1700000000123456)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Delete(ctx, id) })
	if got, err := s.Get(ctx, id); err != nil || got != (Session{StaffID: 42, Stamp: 1700000000123456}) {
		t.Errorf("Get = %+v, %v; want staff 42 with its stamp", got, err)
	}
}
```

Run: `make test-go PKG=./internal/auth RUN=SessionOldFormat`
Expected: build failure, `s.Get undefined`.

- [ ] **Step 2: Implement sessions with a stamp**

In `backend/internal/auth/session.go`, replace `Create` and `Staff` with the following (add `"strconv"` and `"strings"` to the imports):

```go
// Session is a signed-in staff member. Stamp is their password_changed_at in Unix microseconds when the session
// began: after a password change or reset the row's stamp differs and the API refuses the session (FR-A10).
type Session struct {
	StaffID int64
	Stamp   int64
}

// Create starts a session and returns its ID for the cookie (128 random bits).
func (s *Sessions) Create(ctx context.Context, staffID, stamp int64) (string, error) {
	id := rand.Text()
	v := strconv.FormatInt(staffID, 10) + ":" + strconv.FormatInt(stamp, 10)
	if err := s.Redis.Set(ctx, sessionKey(id), v, SessionTTL).Err(); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// Get returns a live session, or ErrNotFound. A value in an older format counts as not found.
func (s *Sessions) Get(ctx context.Context, id string) (Session, error) {
	v, err := s.Redis.Get(ctx, sessionKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	staff, stamp, ok := strings.Cut(v, ":")
	staffID, err1 := strconv.ParseInt(staff, 10, 64)
	st, err2 := strconv.ParseInt(stamp, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return Session{}, ErrNotFound
	}
	return Session{StaffID: staffID, Stamp: st}, nil
}
```

- [ ] **Step 3: Write the rate limiter**

`backend/internal/auth/limit.go`:

```go
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// FR-A11: failed sign-ins count per username and per IP, in a fixed window that starts at the first failure.
const (
	LoginWindow     = 15 * time.Minute
	maxUserFailures = 5
	maxIPFailures   = 20
)

// failKey hashes the value, so keys stay short whatever a client types.
func failKey(kind, v string) string {
	sum := sha256.Sum256([]byte(v))
	return "loginfail:" + kind + ":" + hex.EncodeToString(sum[:])
}

// LoginBlocked reports whether username or ip has reached its failure limit in the current window.
func (s *Sessions) LoginBlocked(ctx context.Context, username, ip string) (bool, error) {
	vals, err := s.Redis.MGet(ctx, failKey("user", username), failKey("ip", ip)).Result()
	if err != nil {
		return false, fmt.Errorf("read login failures: %w", err)
	}
	count := func(v any) int {
		str, _ := v.(string)
		n, _ := strconv.Atoi(str)
		return n
	}
	return count(vals[0]) >= maxUserFailures || count(vals[1]) >= maxIPFailures, nil
}

// LoginFailed counts one failed sign-in against username and ip.
func (s *Sessions) LoginFailed(ctx context.Context, username, ip string) error {
	pipe := s.Redis.TxPipeline()
	for _, k := range []string{failKey("user", username), failKey("ip", ip)} {
		pipe.Incr(ctx, k)
		pipe.ExpireNX(ctx, k, LoginWindow)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("count login failure: %w", err)
	}
	return nil
}

// LoginSucceeded clears the username's failures. The IP's count stays until its window ends.
func (s *Sessions) LoginSucceeded(ctx context.Context, username string) error {
	if err := s.Redis.Del(ctx, failKey("user", username)).Err(); err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}
	return nil
}
```

Run: `make test-go PKG=./internal/auth`
Expected: PASS. (`internal/api` does not build yet; `requireStaff` still calls `Sessions.Staff`.)

- [ ] **Step 4: Rewrite the API auth tests (failing)**

In `backend/internal/api/helpers_test.go`, add `ip string // client IP for sign-ins; set by newAuthEnv` to `testEnv`, next to `sessions`.

Replace the top of `backend/internal/api/auth_test.go` (imports, constants, `newAuthEnv`, `session`, `login`) and every `Test*` in it with the code below. Keep `testPassword`, `testHash`, `newStaff`, `dropStaff`, `cookieNamed`, `withCookie` and `auditRow` as they are.

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

// randomIP is a client address of its own, so the per-IP login limit (FR-A11) never carries over between tests or runs.
func randomIP() string { return fmt.Sprintf("10.%d.%d.%d", rand.IntN(256), rand.IntN(256), rand.IntN(256)) }

// newAuthEnv is a testEnv with Redis sessions and its own client IP.
func newAuthEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	e.ip = randomIP()
	e.mux = http.NewServeMux()
	e.sessions = &auth.Sessions{Redis: rdb}
	(&Server{DB: e.db, UploadDir: e.dir, Sessions: e.sessions}).Routes(e.mux)
	return e
}

// session signs st in without the login endpoint (no login audit row) and returns the cookie.
func (e *testEnv) session(st models.Staff) *http.Cookie {
	e.t.Helper()
	id, err := e.sessions.Create(context.Background(), st.ID, st.PasswordChangedAt.UnixMicro())
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = e.sessions.Delete(context.Background(), id) })
	return &http.Cookie{Name: sessionCookie, Value: id}
}

// login posts username and password to /api/auth/login from the env's IP.
func (e *testEnv) login(username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(loginInput{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = e.ip + ":40000"
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// FR-R1: an active staff member signs in with username and password (the username ignores case and surrounding
// spaces), gets an HttpOnly SameSite=Strict session, and login.success is audited.
func TestLogin_Success_FRR1(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)

	rec := e.login("  "+strings.ToUpper(st.Username)+" ", testPassword)

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"must_change_password":false}` {
		t.Fatalf("login = %d %s, want 200 must_change_password false", rec.Code, rec.Body)
	}
	c := cookieNamed(rec, sessionCookie)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("session cookie = %+v, want HttpOnly, SameSite=Strict, Path=/", c)
	}
	actor, staffID, ip := e.auditRow("login.success", st.Username)
	if actor != "staff" || staffID == nil || *staffID != st.ID || ip == nil || *ip != e.ip {
		t.Errorf("login.success audit = actor %q staff %v ip %v, want staff %d from %s", actor, staffID, ip, st.ID, e.ip)
	}

	me := e.withCookie(http.MethodGet, "/api/auth/me", c)
	var out meOutput
	if me.Code != http.StatusOK || json.Unmarshal(me.Body.Bytes(), &out) != nil {
		t.Fatalf("GET /api/auth/me = %d %s", me.Code, me.Body)
	}
	if out.ID != st.ID || out.Username != st.Username || out.Role != "Agent" || out.MustChangePassword ||
		!slices.Contains(out.Permissions, "ticket.view_all") || slices.Contains(out.Permissions, "staff.create") {
		t.Errorf("me = %+v, want staff %d (%s), role Agent, Agent permissions", out, st.ID, st.Username)
	}
}

// FR-A2: an unknown username, a wrong password, a deactivated account and an account without a password all get
// the same 401 auth.invalid, no session, and a login.failed row with actor guest.
func TestLogin_Refused_FRA2(t *testing.T) {
	e := newAuthEnv(t)
	active, off, nopass := e.newStaff("Agent", true), e.newStaff("Agent", false), e.newStaff("Agent", true)
	e.db.Exec("UPDATE staff SET password_hash = '' WHERE id = ?", nopass.ID)
	tests := []struct{ name, username, password string }{
		{"unknown username", fmt.Sprintf("nobody%d", time.Now().UnixNano()), testPassword},
		{"wrong password", active.Username, "wrong-password-1"},
		{"deactivated", off.Username, testPassword},
		{"no password set", nopass.Username, ""},
		{"empty", "", ""},
	}
	var first string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := e.login(tt.username, tt.password)
			if code, _ := errorBody(rec); rec.Code != http.StatusUnauthorized || code != "auth.invalid" {
				t.Fatalf("login = %d %s, want 401 auth.invalid", rec.Code, rec.Body)
			}
			if first == "" {
				first = rec.Body.String()
			} else if rec.Body.String() != first {
				t.Errorf("body %q differs from %q; failures must look alike", rec.Body, first)
			}
			if cookieNamed(rec, sessionCookie) != nil {
				t.Errorf("refused login set a session cookie")
			}
			if tt.username == "" {
				return
			}
			if actor, staffID, ip := e.auditRow("login.failed", tt.username); actor != "guest" || staffID != nil || ip == nil {
				t.Errorf("login.failed audit = actor %q staff %v ip %v, want guest, no staff, an IP", actor, staffID, ip)
			}
		})
	}
}

// Login CSRF: only a JSON body signs in; a cross-site HTML form can send only form or text bodies.
func TestLogin_JSONOnly_FRR1(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	body := fmt.Sprintf(`{"username": %q, "password": %q}`, st.Username, testPassword)
	for _, ct := range []string{"application/x-www-form-urlencoded", "text/plain", "multipart/form-data; boundary=x", ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		req.RemoteAddr = e.ip + ":40000"
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType || cookieNamed(rec, sessionCookie) != nil {
			t.Errorf("Content-Type %q: login = %d, want 415 and no session", ct, rec.Code)
		}
	}
}

// FR-A11: after 5 failures for a username the next attempt gets 429 even with the right password. A success
// before that clears the count. Another username from the same IP still signs in.
func TestLogin_UsernameLimit_FRA11(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	fail := func(n int) {
		t.Helper()
		for range n {
			if rec := e.login(st.Username, "wrong-password-1"); rec.Code != http.StatusUnauthorized {
				t.Fatalf("wrong password = %d, want 401", rec.Code)
			}
		}
	}
	fail(4)
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusOK {
		t.Fatalf("5th attempt, right password = %d, want 200", rec.Code)
	}
	fail(5) // the success cleared the first four
	rec := e.login(st.Username, testPassword)
	if code, _ := errorBody(rec); rec.Code != http.StatusTooManyRequests || code != "auth.too_many_attempts" || cookieNamed(rec, sessionCookie) != nil {
		t.Fatalf("6th attempt = %d %s, want 429 auth.too_many_attempts and no session", rec.Code, rec.Body)
	}
	if rec := e.login(e.newStaff("Agent", true).Username, testPassword); rec.Code != http.StatusOK {
		t.Errorf("another username from the same IP = %d, want 200", rec.Code)
	}
}

// FR-A11: 20 failures from one IP block every sign-in from it for the window; another IP still signs in.
func TestLogin_IPLimit_FRA11(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	for i := range 20 { // counted straight into Redis: 20 real failures would cost 20 slow hash checks
		if err := e.sessions.LoginFailed(context.Background(), fmt.Sprintf("guess%d-%d", i, time.Now().UnixNano()), e.ip); err != nil {
			t.Fatal(err)
		}
	}
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login from a blocked IP = %d, want 429", rec.Code)
	}
	e.ip = randomIP()
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusOK {
		t.Errorf("login from another IP = %d, want 200", rec.Code)
	}
}

// FR-R1: signing out ends the session.
func TestLogout_FRR1(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Viewer", true)
	c := cookieNamed(e.login(st.Username, testPassword), sessionCookie)

	if rec := e.withCookie(http.MethodPost, "/api/auth/logout", c); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /api/auth/logout = %d", rec.Code)
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", rec.Code)
	}
}

// NFR-9: deactivating a signed-in staff member ends their access on the next request.
func TestSession_DeactivatedStaffLosesAccess_NFR9(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	c := e.session(st)

	e.db.Exec("UPDATE staff SET is_active = false WHERE id = ?", st.ID)

	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after deactivation = %d, want 401", rec.Code)
	}
}

// FR-A10: a session begun before the password changed is refused.
func TestSession_PasswordChangedElsewhere_FRA10(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	c := e.session(st)

	e.db.Exec("UPDATE staff SET password_changed_at = now() WHERE id = ?", st.ID)

	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after a password change elsewhere = %d, want 401", rec.Code)
	}
}

// FR-A8: with a temporary password, me and sign-out work, but every other staff call gets 403.
func TestSession_TemporaryPassword_FRA8(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Root Admin", true)
	e.db.Exec("UPDATE staff SET must_change_password = true WHERE id = ?", st.ID)
	c := e.session(st)

	var me meOutput
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &me) != nil || !me.MustChangePassword {
		t.Fatalf("me = %d %s, want 200 with must_change_password", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/staff/tickets", "/api/staff/accounts", "/api/staff/roles"} {
		rec := e.withCookie(http.MethodGet, p, c)
		if code, _ := errorBody(rec); rec.Code != http.StatusForbidden || code != "auth.password_change_required" {
			t.Errorf("GET %s = %d %s, want 403 auth.password_change_required", p, rec.Code, rec.Body)
		}
	}
	if rec := e.withCookie(http.MethodPost, "/api/auth/logout", c); rec.Code != http.StatusNoContent {
		t.Errorf("logout = %d, want 204", rec.Code)
	}
}
```

Update every other caller:

```bash
cd backend/internal/api
sed -i 's/e, _ := newAuthEnv(t)/e := newAuthEnv(t)/; s/e, idp := newAuthEnv(t)/e := newAuthEnv(t)/' *_test.go
```

In `audit_coverage_test.go`:
- The map type becomes `map[string]func(e *testEnv) []wantAudit`.
- Run `sed -i 's/func(e \*testEnv, _ \*httptest.Server)/func(e *testEnv)/' audit_coverage_test.go`.
- The loop calls `call(e)`.
- Drop the `net/http/httptest` import.
- Replace the `"GET /api/auth/callback"` case with:

```go
		"POST /api/auth/login": func(e *testEnv) []wantAudit {
			st := e.newStaff("Viewer", true)
			if rec := e.login(st.Username, testPassword); rec.Code != http.StatusOK {
				e.t.Fatalf("login = %d %s", rec.Code, rec.Body)
			}
			nobody := fmt.Sprintf("nobody%d", time.Now().UnixNano())
			if rec := e.login(nobody, testPassword); rec.Code != http.StatusUnauthorized {
				e.t.Fatalf("unknown login = %d, want 401", rec.Code)
			}
			return []wantAudit{{action: "login.success", target: st.Username}, {action: "login.failed", target: nobody}}
		},
```

In `staff_test.go`, replace `TestStaffSignIn_FRA2`:

```go
// FR-A2, FR-A8, NFR-9: an account Root Admin creates signs in with its temporary password and can then only change
// it. Once deactivated, its open session gets 401 and it cannot sign in again.
func TestStaffSignIn_FRA2(t *testing.T) {
	e := newAuthEnv(t)
	root := e.session(e.newStaff("Root Admin", true))
	username := fmt.Sprintf("new%d", time.Now().UnixNano())
	rec := e.sendJSON(http.MethodPost, "/api/staff/accounts", root,
		fmt.Sprintf(`{"name": "New Agent", "username": %q, "role_id": %d, "password": %q}`, username, e.roleID("Agent"), testPassword))
	var acc staffAccount
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &acc) != nil {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() { e.dropStaff(acc.ID) })

	login := e.login(username, testPassword)
	if login.Code != http.StatusOK || strings.TrimSpace(login.Body.String()) != `{"must_change_password":true}` {
		t.Fatalf("new account login = %d %s, want 200 must_change_password true", login.Code, login.Body)
	}
	c := cookieNamed(login, sessionCookie)
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200", rec.Code)
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/tickets", c); rec.Code != http.StatusForbidden {
		t.Errorf("queue before changing the temporary password = %d, want 403", rec.Code)
	}
	if rec := e.sendJSON(http.MethodPatch, accountPath(acc.ID), root, `{"is_active": false}`); rec.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", rec.Code, rec.Body)
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after deactivation = %d, want 401", rec.Code)
	}
	if rec := e.login(username, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("deactivated login = %d, want 401", rec.Code)
	}
}
```

Run: `make test-go PKG=./internal/api RUN='Login|Logout|Session|StaffSignIn|AuditCoverage'`
Expected: build failure (`loginInput`, `requireSession` and `Sessions.Get` usage are missing in `auth.go`).

- [ ] **Step 5: Rewrite auth.go**

Replace everything in `backend/internal/api/auth.go` above `// require lets a request through...`. Keep `require`, `currentStaff`, `meOutput` and `me` from Task 2.

```go
package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

const sessionCookie = "ticket_session"

// sessionCookieFor sets (maxAge > 0) or clears (maxAge < 0) the session cookie. SameSite=Strict: sign-in never
// comes back from another site, so the cookie never needs to ride a cross-site request.
func (s *Server) sessionCookieFor(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.SecureCookies, SameSite: http.SameSiteStrictMode,
	}
}

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// auditName is a typed username fit for the activity log: no control or bidi characters, at most 64 characters.
func auditName(s string) string {
	bad := badRune("")
	s = strings.Map(func(r rune) rune {
		if bad(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 64 {
		s = string(r[:64])
	}
	return s
}

// POST /api/auth/login {"username", "password"} — staff sign in (FR-R1, FR-A2, FR-A11). Every failure gets the same
// 401 after one full password check, so neither the answer nor its timing tells which usernames exist.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// A cross-site HTML form cannot send application/json, and this API never allows CORS, so another site cannot
	// sign a browser in to an account of its choosing (login CSRF).
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_body")
		return
	}
	var in loginInput
	if !decodeBody(w, r, &in) {
		return
	}
	ctx, ip := r.Context(), clientIP(r)
	username := auth.NormalizeUsername(in.Username)

	blocked, err := s.Sessions.LoginBlocked(ctx, username, ip)
	if err != nil {
		slog.Error("sign in", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if blocked { // not audited: no password was checked, and an attacker could otherwise fill audit_log at will
		writeError(w, http.StatusTooManyRequests, "auth.too_many_attempts")
		return
	}

	var st models.Staff
	err = s.DB.WithContext(ctx).Where("username = ? AND is_active", username).First(&st).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("find staff", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// st.PasswordHash is "" for an unknown or deactivated username; CheckPassword still does a full check (FR-A2).
	if !auth.CheckPassword(st.PasswordHash, in.Password) {
		if err := s.Sessions.LoginFailed(ctx, username, ip); err != nil {
			slog.Error("sign in", "error", err)
		}
		// Not signed in, so the actor is guest.
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return audit.Record(tx, audit.Guest, "login.failed", audit.Change{Target: auditName(username), IP: ip})
		})
		if err != nil {
			slog.Error("audit login.failed", "error", err)
		}
		writeError(w, http.StatusUnauthorized, "auth.invalid")
		return
	}

	id, err := s.Sessions.Create(ctx, st.ID, st.PasswordChangedAt.UnixMicro())
	if err != nil {
		slog.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return audit.Record(tx, audit.Staff(st.ID), "login.success", audit.Change{Target: st.Username, IP: ip})
	})
	if err != nil { // no session without its audit row (FR-L1)
		slog.Error("audit login.success", "error", err)
		_ = s.Sessions.Delete(ctx, id)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := s.Sessions.LoginSucceeded(ctx, username); err != nil {
		slog.Error("sign in", "error", err)
	}
	http.SetCookie(w, s.sessionCookieFor(id, int(auth.SessionTTL.Seconds())))
	writeJSON(w, http.StatusOK, map[string]bool{"must_change_password": st.MustChangePassword})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Sessions.Delete(r.Context(), c.Value); err != nil {
			slog.Error("sign out", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
	}
	http.SetCookie(w, s.sessionCookieFor("", -1))
	w.WriteHeader(http.StatusNoContent)
}

type staffKey struct{}

// requireSession lets a request through only with a live session of an active staff member whose password has not
// changed since that session began. The staff row is read on every request, so deactivation (NFR-9) and a password
// change or reset (FR-A10) end access at once.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "auth.required")
			return
		}
		sess, err := s.Sessions.Get(ctx, c.Value)
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "auth.required")
			return
		}
		if err != nil {
			slog.Error("read session", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		var st models.Staff
		err = s.DB.WithContext(ctx).Where("id = ? AND is_active", sess.StaffID).First(&st).Error
		if err == nil && st.PasswordChangedAt.UnixMicro() != sess.Stamp {
			err = gorm.ErrRecordNotFound
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = s.Sessions.Delete(ctx, c.Value)
			writeError(w, http.StatusUnauthorized, "auth.required")
			return
		}
		if err != nil {
			slog.Error("read staff", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		next(w, r.WithContext(context.WithValue(ctx, staffKey{}, &st)))
	}
}

// requireStaff is requireSession for every staff route except me and the password change: staff who still hold a
// temporary password get 403 until they choose their own (FR-A8).
func (s *Server) requireStaff(next http.HandlerFunc) http.HandlerFunc {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if currentStaff(r).MustChangePassword {
			writeError(w, http.StatusForbidden, "auth.password_change_required")
			return
		}
		next(w, r)
	})
}
```

In `backend/internal/api/api.go`, replace the four auth routes with:

```go
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.requireSession(s.me))
```

- [ ] **Step 6: Run the tests and confirm they pass**

Run: `make test-go PKG=./internal/api` then `make test-go PKG=./internal/auth`
Expected: all PASS. (`OIDC` stays in `Server` and `config` until Task 6; nothing reads it.)

- [ ] **Step 7: Checkpoint**

Run: `make test`
Expected: PASS. From here until Task 7, e2e is expected to fail: the login page still posts to the removed SSO route.

---

### Task 4: Change own password

**Files:**
- Modify: `backend/internal/api/auth.go` (append `changePassword`)
- Modify: `backend/internal/api/api.go` (route)
- Create: `backend/internal/api/password_test.go`
- Modify: `backend/internal/api/audit_coverage_test.go` (case)

**Interfaces:**
- Consumes: Task 3 (`requireSession`, `sessionCookieFor`, `Sessions.Create/LoginBlocked/LoginFailed`)
- Produces: `POST /api/auth/password {current_password, new_password}` returns 204 with a new cookie, or 400 validation with fields `current_password: wrong|required` and `new_password: required|too_short|too_long|same`, or 429.

- [ ] **Step 1: Write the failing tests**

`backend/internal/api/password_test.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"testing"

	"ticket-app/internal/auth"
)

func passwordBody(current, next string) string {
	return fmt.Sprintf(`{"current_password": %q, "new_password": %q}`, current, next)
}

// FR-A8, FR-A9, FR-A10: a staff member with a temporary password replaces it. This browser gets a new session,
// every other session ends, the old password stops working, and staff.password_changed is audited.
func TestChangePassword_FRA8(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	e.db.Exec("UPDATE staff SET must_change_password = true WHERE id = ?", st.ID)
	here, other := e.session(st), e.session(st)
	next := "brand-new-password-1"

	rec := e.sendJSON(http.MethodPost, "/api/auth/password", here, passwordBody(testPassword, next))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /api/auth/password = %d %s, want 204", rec.Code, rec.Body)
	}
	fresh := cookieNamed(rec, sessionCookie)
	if fresh == nil || fresh.Value == here.Value {
		t.Fatalf("no new session cookie: %+v", fresh)
	}
	for name, c := range map[string]*http.Cookie{"this browser's old session": here, "another session": other} {
		if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s after the change = %d, want 401", name, rec.Code)
		}
	}
	var me meOutput
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", fresh); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &me) != nil || me.MustChangePassword {
		t.Errorf("me with the new session = %d %s, want 200 without must_change_password", rec.Code, rec.Body)
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/tickets", fresh); rec.Code != http.StatusOK {
		t.Errorf("queue after the change = %d, want 200", rec.Code)
	}
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password = %d, want 401", rec.Code)
	}
	if rec := e.login(st.Username, next); rec.Code != http.StatusOK {
		t.Errorf("new password = %d, want 200", rec.Code)
	}
	if actor, id, ip := e.auditRow("staff.password_changed", st.Username); actor != "staff" || id == nil || *id != st.ID || ip == nil {
		t.Errorf("staff.password_changed audit = %s %v %v, want the staff member with an IP", actor, id, ip)
	}
}

// FR-A7, FR-A9: the current password must be right and the new one must fit the policy and differ. A refused
// change keeps the password and the session.
func TestChangePassword_Refused_FRA9(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	c := e.session(st)
	tests := []struct {
		name   string
		body   string
		fields map[string]string
	}{
		{"wrong current", passwordBody("wrong-password-1", "brand-new-password-1"), map[string]string{"current_password": "wrong"}},
		{"too short", passwordBody(testPassword, "short"), map[string]string{"new_password": "too_short"}},
		{"too long", passwordBody(testPassword, fmt.Sprintf("%0129d", 0)), map[string]string{"new_password": "too_long"}},
		{"same", passwordBody(testPassword, testPassword), map[string]string{"new_password": "same"}},
		{"missing", `{}`, map[string]string{"current_password": "wrong", "new_password": "required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := e.sendJSON(http.MethodPost, "/api/auth/password", c, tt.body)
			if code, fields := errorBody(rec); rec.Code != http.StatusBadRequest || code != "validation" || !maps.Equal(fields, tt.fields) {
				t.Fatalf("POST = %d %s, want 400 validation %v", rec.Code, rec.Body, tt.fields)
			}
		})
	}
	if row := e.staffRow(st.ID); !auth.CheckPassword(row.PasswordHash, testPassword) {
		t.Errorf("a refused change altered the password")
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusOK {
		t.Errorf("session after refused changes = %d, want 200", rec.Code)
	}
}
```

Add a case to `TestAuditCoverage_FRL1` in `audit_coverage_test.go`:

```go
		"POST /api/auth/password": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true)
			if rec := e.sendJSON(http.MethodPost, "/api/auth/password", e.session(st), `{"current_password": "`+testPassword+`", "new_password": "another-password-1"}`); rec.Code != http.StatusNoContent {
				e.t.Fatalf("POST password = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "staff.password_changed", target: st.Username}}
		},
```

Run: `make test-go PKG=./internal/api RUN='ChangePassword|AuditCoverage'`
Expected: FAIL (404: the route does not exist).

- [ ] **Step 2: Implement**

Append to `backend/internal/api/auth.go` (add `"time"` to the imports):

```go
// POST /api/auth/password {"current_password", "new_password"} — staff replace their own password (FR-A8, FR-A9).
// Wrong current passwords count toward the login limit (FR-A11). Every other session of this staff member ends
// (FR-A10); this browser gets a new session.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	ctx, me, ip := r.Context(), currentStaff(r), clientIP(r)
	blocked, err := s.Sessions.LoginBlocked(ctx, me.Username, ip)
	if err != nil {
		slog.Error("change password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if blocked {
		writeError(w, http.StatusTooManyRequests, "auth.too_many_attempts")
		return
	}
	fields := map[string]string{}
	if p := auth.PasswordProblem(in.New); p != "" {
		fields["new_password"] = p
	} else if in.New == in.Current {
		fields["new_password"] = "same"
	}
	if !auth.CheckPassword(me.PasswordHash, in.Current) {
		fields["current_password"] = "wrong"
		if err := s.Sessions.LoginFailed(ctx, me.Username, ip); err != nil {
			slog.Error("change password", "error", err)
		}
	}
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		slog.Error("change password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	var changed time.Time
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`UPDATE staff SET password_hash = ?, must_change_password = false, password_changed_at = now()
			WHERE id = ? RETURNING password_changed_at`, hash, me.ID).Scan(&changed).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Staff(me.ID), "staff.password_changed", audit.Change{Target: me.Username, IP: ip})
	})
	if err != nil {
		slog.Error("change password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// The new stamp already ends every older session, this one included; the explicit delete just tidies Redis.
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.Sessions.Delete(ctx, c.Value)
	}
	id, err := s.Sessions.Create(ctx, me.ID, changed.UnixMicro())
	if err != nil { // the password did change; the staff member signs in again with it
		slog.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	http.SetCookie(w, s.sessionCookieFor(id, int(auth.SessionTTL.Seconds())))
	w.WriteHeader(http.StatusNoContent)
}
```

In `api.go`, after the `me` route:

```go
	mux.HandleFunc("POST /api/auth/password", s.requireSession(s.changePassword)) // works with a temporary password (FR-A8)
```

- [ ] **Step 3: Run the tests and confirm they pass**

Run: `make test-go PKG=./internal/api RUN='ChangePassword|AuditCoverage'`
Expected: PASS.

- [ ] **Step 4: Checkpoint**

Run: `make test`
Expected: PASS.

---

### Task 5: Root Admin resets a password

**Files:**
- Modify: `backend/internal/api/staff.go` (append `resetPassword`)
- Modify: `backend/internal/api/api.go` (route)
- Modify: `backend/internal/api/staff_test.go` (tests), `audit_coverage_test.go` (case)

**Interfaces:**
- Consumes: Task 2 (`writeStaffError`), Task 3 (sessions stamped with `password_changed_at`)
- Produces: `PUT /api/staff/accounts/{id}/password {password}` returns 204, or 400 validation `{password}`, 403 or 404 `staff.not_found`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/api/staff_test.go`:

```go
// FR-A9, FR-A10, NFR-9: only Root Admin resets a password. The account gets a temporary password it must change,
// its open sessions end, and staff.password_reset is audited without the password.
func TestResetPassword_FRA9(t *testing.T) {
	e := newAuthEnv(t)
	rootSt := e.newStaff("Root Admin", true)
	root, admin := e.session(rootSt), e.session(e.newStaff("Admin", true))
	st := e.newStaff("Agent", true)
	old := e.session(st)
	path, temp := accountPath(st.ID)+"/password", "temporary-pass-1"

	tests := []struct {
		name, path, body string
		c                *http.Cookie
		code             int
		err              string
		fields           map[string]string
	}{
		{"Admin refused", path, `{"password": "` + temp + `"}`, admin, 403, "auth.forbidden", nil},
		{"too short", path, `{"password": "short"}`, root, 400, "validation", map[string]string{"password": "too_short"}},
		{"missing", path, `{}`, root, 400, "validation", map[string]string{"password": "required"}},
		{"unknown staff", accountPath(999999999) + "/password", `{"password": "` + temp + `"}`, root, 404, "staff.not_found", nil},
	}
	for _, tt := range tests {
		rec := e.sendJSON(http.MethodPut, tt.path, tt.c, tt.body)
		if code, fields := errorBody(rec); rec.Code != tt.code || code != tt.err || !maps.Equal(fields, tt.fields) {
			t.Errorf("%s: PUT = %d %s, want %d %s %v", tt.name, rec.Code, rec.Body, tt.code, tt.err, tt.fields)
		}
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", old); rec.Code != http.StatusOK {
		t.Fatalf("refused resets ended the session: me = %d", rec.Code)
	}

	if rec := e.sendJSON(http.MethodPut, path, root, `{"password": "`+temp+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Root Admin reset = %d %s, want 204", rec.Code, rec.Body)
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", old); rec.Code != http.StatusUnauthorized {
		t.Errorf("session from before the reset = %d, want 401", rec.Code)
	}
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusUnauthorized {
		t.Errorf("old password after reset = %d, want 401", rec.Code)
	}
	if rec := e.login(st.Username, temp); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"must_change_password":true}` {
		t.Errorf("temporary password = %d %s, want 200 must_change_password true", rec.Code, rec.Body)
	}
	if actor, id, _ := e.auditRow("staff.password_reset", st.Username); actor != "staff" || id == nil || *id != rootSt.ID {
		t.Errorf("staff.password_reset actor = %s %v, want the Root Admin", actor, id)
	}
	var leaked int64
	e.db.Raw("SELECT count(*) FROM audit_log WHERE concat(target, from_value, to_value) LIKE ?", "%"+temp+"%").Scan(&leaked)
	if leaked != 0 {
		t.Errorf("%d audit rows contain the password", leaked)
	}
}

// FR-A9, Review Focus: a Root Admin who resets their own password is signed out, not locked out: the temporary
// password signs in and must then be changed.
func TestResetPassword_Self_FRA9(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Root Admin", true)
	c := e.session(st)
	if rec := e.sendJSON(http.MethodPut, accountPath(st.ID)+"/password", c, `{"password": "my-temporary-pass"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("self reset = %d %s", rec.Code, rec.Body)
	}
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusUnauthorized {
		t.Errorf("own session after self reset = %d, want 401", rec.Code)
	}
	if rec := e.login(st.Username, "my-temporary-pass"); rec.Code != http.StatusOK {
		t.Errorf("sign in with the temporary password = %d, want 200", rec.Code)
	}
}
```

Add a case to `TestAuditCoverage_FRL1`:

```go
		"PUT /api/staff/accounts/{id}/password": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true)
			if rec := e.sendJSON(http.MethodPut, accountPath(st.ID)+"/password", e.session(e.newStaff("Root Admin", true)), `{"password": "temporary-pass-1"}`); rec.Code != http.StatusNoContent {
				e.t.Fatalf("PUT password = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "staff.password_reset", target: st.Username}}
		},
```

Run: `make test-go PKG=./internal/api RUN='ResetPassword|AuditCoverage'`
Expected: FAIL (404 or 405: no route).

- [ ] **Step 2: Implement**

Append to `backend/internal/api/staff.go`:

```go
// PUT /api/staff/accounts/{id}/password {"password"} — Root Admin gives a staff member a temporary password (FR-A9).
// They must change it at their next sign-in (FR-A8), and their open sessions end now (FR-A10, NFR-9).
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "staff.not_found")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if p := auth.PasswordProblem(in.Password); p != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"password": p}})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeStaffError(w, err, "hash password")
		return
	}
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var username string
		if err := tx.Raw(`UPDATE staff SET password_hash = ?, must_change_password = true, password_changed_at = now()
			WHERE id = ? RETURNING username`, hash, id).Scan(&username).Error; err != nil {
			return err
		}
		if username == "" {
			return gorm.ErrRecordNotFound
		}
		return audit.Record(tx, audit.Staff(currentStaff(r).ID), "staff.password_reset", audit.Change{Target: username, IP: clientIP(r)})
	})
	if err != nil {
		writeStaffError(w, err, "reset password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

In `api.go`, after the `PATCH /api/staff/accounts/{id}` route:

```go
	mux.HandleFunc("PUT /api/staff/accounts/{id}/password", s.require(rbac.StaffCreate, s.resetPassword)) // Root Admin only (FR-A9)
```

- [ ] **Step 3: Run the tests and confirm they pass**

Run: `make test-go PKG=./internal/api RUN='ResetPassword|AuditCoverage|StaffRoutes'`
Expected: PASS. `TestStaffRoutes_FRR3` confirms that a Viewer gets 403 on the new route.

- [ ] **Step 4: Checkpoint**

Run: `make test`
Expected: PASS.

---

### Task 6: Remove SSO

**Files:**
- Delete: `backend/internal/auth/oidc.go`, `backend/internal/auth/oidcmock/`, `backend/cmd/dev-oidc/`
- Modify: `backend/internal/auth/session.go` (drop `LoginTTL`, `PendingLogin`, `SaveLogin`, `TakeLogin`; package doc)
- Modify: `backend/internal/api/api.go` (`OIDC` field), `backend/cmd/ticket-app/main.go`, `backend/internal/config/config.go`, `backend/internal/config/config_test.go`
- Modify: `Makefile`, `frontend/playwright.config.ts`, `.env.example`, `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Consumes: Tasks 3 to 5 (nothing reads OIDC any more)
- Produces: `config.Config` without `OIDCIssuer`, `OIDCClientID` and `OIDCClientSecret`, and `api.Server` without `OIDC`.

- [ ] **Step 1: Change the config test first (failing)**

In `backend/internal/config/config_test.go`:
- Remove the three `OIDC_*` entries from `valid()`.
- Change the "all missing" row to `[]string{"DATABASE_URL", "REDIS_URL", "UPLOAD_DIR", "BASE_URL"}`.
- Add this test:

```go
// FR-R1: there is no SSO; OIDC settings are not needed.
func TestLoad_NoOIDC_FRR1(t *testing.T) {
	if _, err := Load(env(valid())); err != nil {
		t.Fatalf("Load() without OIDC_* = %v, want no error", err)
	}
}
```

Run: `cd backend && go test ./internal/config -run 'Load' -v`
Expected: FAIL (`missing environment variables: OIDC_ISSUER, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET`).

- [ ] **Step 2: Remove OIDC from config, server and main**

- `config.go`: delete the three `OIDC*` fields and their `req(...)` lines.
- `api.go`: delete `OIDC *auth.OIDC` from `Server`.
- `main.go`: replace the `app := &api.Server{...}` literal with:

```go
	app := &api.Server{
		DB:            db,
		UploadDir:     cfg.UploadDir,
		Sessions:      &auth.Sessions{Redis: rdb},
		SecureCookies: strings.HasPrefix(base, "https://"),
	}
```

- `session.go`: delete `LoginTTL`, `PendingLogin`, `SaveLogin` and `TakeLogin`, and the `encoding/json` import. Change the package doc to `// Package auth checks staff passwords (FR-A7) and keeps sessions and failed sign-in counts in Redis.` and the `Sessions` doc to `// Sessions stores staff sessions and failed sign-in counts in Redis.`
- Delete the files:

```bash
rm backend/internal/auth/oidc.go
rm -r backend/internal/auth/oidcmock backend/cmd/dev-oidc
cd backend && go mod tidy
```

Expected: `go.mod` no longer lists `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2` or `github.com/go-jose/go-jose/v4`.

- [ ] **Step 3: Remove the dev IdP from Makefile, Playwright and .env.example**

`Makefile`:
- Line 1: remove `dev-oidc` from `.PHONY`.
- Replace the `dev` comment and target, and delete the `dev-oidc` comment and target:

```makefile
# Runs the Go API on :8080 and the SvelteKit dev server on :5173 (proxies /api and /healthz).
dev:
	$(MAKE) -j2 dev-backend dev-frontend
```

`frontend/playwright.config.ts`: delete the dev-oidc comment and its `webServer` entry, leaving the Go API and Vite entries.

`.env.example`: delete the `OIDC_ISSUER`, `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` lines and any comment about the dev sign-in provider. This file is behind a read-deny rule for agents. Try `sed -i '/^OIDC_/d' .env.example`. If that is denied, ask the user to delete those lines. Leftover lines are harmless: nothing reads them.

- [ ] **Step 4: Run the tests and the leftovers check**

Run: `make test` then `make lint`
Expected: PASS.

Run: `grep -rIli oidc backend frontend Makefile --exclude-dir=node_modules --exclude-dir=bin --exclude-dir=.svelte-kit --exclude-dir=build --exclude-dir=dist`
Expected: no output. If `frontend/src/routes/login/+page.svelte` or `.claude/rules/frontend.md` show up, they are handled in Tasks 7 and 8.

---

### Task 7: Frontend

**Files:**
- Modify: `frontend/src/lib/api.ts`
- Rewrite: `frontend/src/routes/login/+page.svelte`
- Create: `frontend/src/routes/staff/password/+page.svelte`
- Modify: `frontend/src/routes/staff/+layout.ts`, `frontend/src/routes/+layout.svelte`
- Modify: `frontend/src/routes/staff/admin/staff/+page.svelte`
- Modify: `frontend/messages/en.json`, `zh-CN.json`, `my.json`, `th.json`
- Modify: `frontend/e2e/helpers.ts`, `staff-login.spec.ts`, `admin.spec.ts`, and the `signIn`/`openTicket` calls in the other specs

**Interfaces:**
- Consumes: the API from Tasks 2 to 5.
- Produces:
  - `Me.username` and `Me.must_change_password`.
  - `login(username, password)`, `changePassword(current, next)` and `resetStaffPassword(id, password)`.
  - `StaffAccount.username`, with `email: string` (`''` when none).
  - `createStaffAccount({name, username, email, role_id, password})`.
  - e2e `signIn(page, username, password = devPassword)`.

- [ ] **Step 1: Update the e2e tests first (failing)**

`frontend/e2e/helpers.ts`, replace `signIn`:

```ts
/** The password `make seed` gives the dev staff root, agent and viewer. */
export const devPassword = 'dev-password';

/** Signs in on /login. Dev staff come from `make seed`. */
export async function signIn(page: Page, username: string, password = devPassword) {
	await page.goto('/login');
	await page.getByLabel('Username').fill(username);
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in' }).click();
}
```

Point the other specs at the usernames:

```bash
cd frontend
sed -i -E "s/(signIn|openTicket)\(page, '(root|agent|viewer)@dev\.test'/\1(page, '\2'/g" e2e/*.spec.ts
grep -rn "@dev.test" e2e
```

Expected remaining hits: only `hasText: '...@dev.test'` row filters in `admin.spec.ts` (rows still show the email) and the account-menu checks fixed below.

Replace `frontend/e2e/staff-login.spec.ts`:

```ts
import { expect, test } from '@playwright/test';
import { signIn } from './helpers';

// T2.15: staff sign in with username and password. Needs `make seed` (dev staff, password dev-password).

// FR-A2: an unknown username gets the same translated message as a wrong password. Only one failed attempt per
// run: failures from 127.0.0.1 count toward the per-IP limit (FR-A11).
test('unknown username is refused', async ({ page }) => {
	await signIn(page, `stranger-${Date.now()}`, 'some-password-1');
	await expect(page.getByRole('alert')).toContainText('Wrong username or password.');
	await expect(page).toHaveURL(/\/login$/);
});

// FR-R1: a staff member signs in, sees their name and username, and signs out.
test('staff member signs in and out', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	// Name, username and sign-out live in the account menu, top right of every staff page.
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.locator('#account-menu')).toContainText('agent');
	await expect(page.locator('#account-menu').getByRole('link', { name: 'Change password' })).toBeVisible();

	await page.getByRole('button', { name: 'Sign out' }).click();
	await expect(page).toHaveURL(/\/login$/);
	await page.goto('/staff');
	await expect(page).toHaveURL(/\/login$/);
});
```

In `frontend/e2e/admin.spec.ts`:

- `deactivate(page, email, name)` becomes `deactivate(page, username, name)`, filtering rows with `hasText: username`. Its `signIn(page, 'root@dev.test')` is already `signIn(page, 'root')` from the sed.
- At the top of the main test, replace `const email = ...` with:

  ```ts
  const username = `e2e-admin-${stamp}`;
  const temp = 'e2e-temp-password';
  const own = 'e2e-own-password-1';
  ```

- Replace the create block (from `await add.getByRole('button', { name: 'Add staff member' }).click();` down to the duplicate-email check) with:

```ts
		await add.getByRole('button', { name: 'Add staff member' }).click();
		await expect(add.getByText('This field is required.')).toHaveCount(4); // name, username, role, temporary password
		await add.getByLabel('Name', { exact: true }).fill(name);
		await add.getByLabel('Username').fill(username);
		await add.getByLabel('Role').selectOption({ label: 'Admin' });
		await add.getByLabel('Temporary password').fill(temp);
		await add.getByRole('button', { name: 'Add staff member' }).click();
		const created = page.getByRole('listitem').filter({ hasText: username });
		await expect(created).toContainText('Active');
		await expect(created).toContainText('No email');
		await expect(created.getByLabel('Role').locator('option:checked')).toHaveText('Admin');
		await expect(add.getByLabel('Username')).toHaveValue('');

		// The same username again, in capitals, is refused with a translated message.
		await add.getByLabel('Name', { exact: true }).fill(name);
		await add.getByLabel('Username').fill(username.toUpperCase());
		await add.getByLabel('Role').selectOption({ label: 'Admin' });
		await add.getByLabel('Temporary password').fill(temp);
		await add.getByRole('button', { name: 'Add staff member' }).click();
		await expect(add.getByText('This username is already taken.')).toBeVisible();
```

- Replace the new Admin's sign-in block (`// The new Admin signs in through the dev provider (FR-A2).` up to the account-menu click) with:

```ts
		// The new Admin signs in with the temporary password and must choose their own first (FR-A2, FR-A8).
		// A deep link does not get around it.
		await page.context().clearCookies();
		await signIn(page, username, temp);
		await expect(page).toHaveURL(/\/staff\/password$/);
		await page.goto('/staff/admin/staff');
		await expect(page).toHaveURL(/\/staff\/password$/);
		await expect(page.getByRole('status')).toContainText('temporary password');
		await axeBothSizes(page, 'change-password-forced');
		await page.getByLabel('Current password').fill(temp);
		await page.getByLabel('New password', { exact: true }).fill(own);
		await page.getByLabel('Repeat new password').fill(own);
		await page.getByRole('button', { name: 'Change password' }).click();
		await expect(page).toHaveURL(/\/staff$/);
```

- Change `await expect(page.getByLabel('Work email')).toHaveCount(0);` to `await expect(page.getByLabel('Temporary password')).toHaveCount(0);`.
- In the agent test, change `toContainText('agent@dev.test')` on `#account-menu` to `toContainText('agent')`.
- Before `} finally {` in the main test, add the Root Admin reset (FR-A9):

```ts
		// FR-A9: Root Admin resets the new Admin's password; the next sign-in must change it again.
		await page.context().clearCookies();
		await signIn(page, 'root');
		await page.goto('/staff/admin/staff');
		const row = page.getByRole('listitem').filter({ hasText: username });
		await row.getByRole('button', { name: 'Reset password' }).click();
		const reset = page.getByRole('dialog');
		await expect(reset).toContainText(`Reset password for ${name}?`);
		await reset.getByLabel('Temporary password').fill(temp);
		await reset.getByRole('button', { name: 'Reset password' }).click();
		await expect(page.getByText('Temporary password set.')).toBeVisible();
		await page.context().clearCookies();
		await signIn(page, username, temp);
		await expect(page).toHaveURL(/\/staff\/password$/);
```

- `finally` calls `deactivate(page, username, name)`.

Run: `cd frontend && npm run test:e2e` (needs `docker compose up -d --wait && make migrate seed`; stop any Go API started before Task 3).
Expected: FAIL (no Username field on /login).

- [ ] **Step 2: Add the message keys**

In `frontend/messages/en.json`:
- Delete `login_err_not_allowed`.
- Change `login_intro`, `login_button` and `staff_email`.
- Add the new keys:

```json
	"login_intro": "For IT staff. Sign in with the username and password a Root Admin gave you.",
	"login_button": "Sign in",
	"login_username": "Username",
	"login_password": "Password",
	"login_err_invalid": "Wrong username or password.",
	"login_err_too_many": "Too many failed attempts. Wait 15 minutes, then try again.",
	"password_heading": "Change password",
	"password_forced": "You signed in with a temporary password. Choose your own password to continue.",
	"password_current": "Current password",
	"password_new": "New password",
	"password_new_helper": "12 to 128 characters. A few unrelated words make a strong password.",
	"password_repeat": "Repeat new password",
	"password_save": "Change password",
	"password_changed": "Password changed.",
	"password_err_wrong": "The current password is wrong.",
	"password_err_too_short": "Use at least 12 characters.",
	"password_err_too_long": "Use at most 128 characters.",
	"password_err_same": "Choose a password different from the current one.",
	"password_err_mismatch": "The two new passwords do not match.",
	"staff_email": "Work email (optional)",
	"staff_username": "Username",
	"staff_username_helper": "3 to 64 characters: a–z, 0–9, dot, dash, underscore or @.",
	"staff_err_username_invalid": "Use 3 to 64 characters: a–z, 0–9, dot, dash, underscore or @.",
	"staff_err_username_taken": "This username is already taken.",
	"staff_no_email": "No email",
	"staff_temp_password": "Temporary password",
	"staff_temp_password_helper": "Tell the person in private. They must change it when they first sign in.",
	"staff_reset_password": "Reset password",
	"staff_reset_title": "Reset password for {name}?",
	"staff_reset_text": "They are signed out everywhere and must choose a new password at their next sign-in.",
	"staff_reset_done": "Temporary password set."
```

Make the same changes in `zh-CN.json`, `my.json` and `th.json` with the **English** text as a placeholder. Replace the old `login_intro` and `login_button` translations too, because they mention Microsoft.

Run: `node scripts/check-i18n.mjs` (from the repo root)
Expected: PASS.

- [ ] **Step 3: API client**

In `frontend/src/lib/api.ts`, replace the staff session block (the `Me` type through `logout`):

```ts
// Staff session (T2.15). The cookie is HttpOnly; these calls sign in and out and ask the API who is signed in.
export type Me = {
	id: number;
	name: string;
	username: string;
	role: string;
	language: string;
	permissions: string[];
	/** A temporary password must be replaced before anything else (FR-A8). */
	must_change_password: boolean;
};

export const getMe = () => call<Me>('/api/auth/me');

/** Errors: auth.invalid (any wrong username or password, FR-A2), auth.too_many_attempts (FR-A11). */
export const login = (username: string, password: string) =>
	call<{ must_change_password: boolean }>('/api/auth/login', sendJSON('POST', { username, password }));

/** Errors: validation with fields current_password (wrong) and new_password (required, too_short, too_long, same). */
export const changePassword = (current_password: string, new_password: string) =>
	call<void>('/api/auth/password', sendJSON('POST', { current_password, new_password }));

export const logout = () => call<unknown>('/api/auth/logout', { method: 'POST' });
```

In the staff accounts block:

```ts
export type StaffAccount = {
	id: number;
	name: string;
	username: string;
	/** '' when none; email is for notifications only. */
	email: string;
	role: NamedItem;
	is_active: boolean;
	created_at: string;
};
```

```ts
/** Root Admin only (FR-A1). The password is temporary (FR-A8). Errors: validation, staff.username_taken, staff.email_taken. */
export const createStaffAccount = (a: { name: string; username: string; email: string; role_id: number; password: string }) =>
	call<StaffAccount>('/api/staff/accounts', sendJSON('POST', a));

/** Root Admin only (FR-A9): a new temporary password; the account's sessions end. Errors: validation, staff.not_found. */
export const resetStaffPassword = (id: number, password: string) =>
	call<void>(`/api/staff/accounts/${id}/password`, sendJSON('PUT', { password }));
```

- [ ] **Step 4: Login page**

Replace `frontend/src/routes/login/+page.svelte`:

```svelte
<!-- Staff sign-in (T2.15, FR-R1): username and password. The API answers every failure the same way (FR-A2). -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError, login } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { m } from '$lib/paraglide/messages';

	let username = $state('');
	let password = $state('');
	let errors = $state<{ username?: string; password?: string }>({});
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		errors = {};
		if (!username.trim()) errors.username = m.err_required();
		if (!password) errors.password = m.err_required();
		if (errors.username || errors.password) return;
		busy = true;
		try {
			const res = await login(username, password);
			await goto(res.must_change_password ? '/staff/password' : '/staff');
		} catch (err) {
			password = '';
			if (!(err instanceof ApiError)) error = m.err_network();
			else if (err.code === 'auth.invalid') error = m.login_err_invalid();
			else if (err.code === 'auth.too_many_attempts') error = m.login_err_too_many();
			else error = m.login_err_failed();
		} finally {
			busy = false;
		}
	}
</script>

<form onsubmit={submit} novalidate>
	<h1>{m.login_heading()}</h1>
	<p>{m.login_intro()}</p>
	{#if error}<Alert variant="error">{error}</Alert>{/if}
	<TextInput
		label={m.login_username()}
		bind:value={username}
		error={errors.username}
		autocomplete="username"
		autocapitalize="none"
		spellcheck="false"
		maxlength={64}
		required
	/>
	<TextInput
		label={m.login_password()}
		type="password"
		bind:value={password}
		error={errors.password}
		autocomplete="current-password"
		required
	/>
	<Button type="submit" disabled={busy}>{m.login_button()}</Button>
</form>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		max-width: 440px;
		margin: var(--space-xl) auto 0;
		padding: var(--space-xl);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
	}
	p {
		margin: 0;
		color: var(--color-muted);
	}
</style>
```

- [ ] **Step 5: Staff layout redirect and account menu**

Replace `frontend/src/routes/staff/+layout.ts`:

```ts
import { redirect } from '@sveltejs/kit';
import { ApiError, getMe, type Me } from '$lib/api';
import type { LayoutLoad } from './$types';

// Staff pages need a session. This only picks the page to show; the API checks every call (FR-R3).
export const load: LayoutLoad = async ({ url }) => {
	let me: Me;
	try {
		me = await getMe();
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) redirect(307, '/login');
		throw err;
	}
	// FR-A8: a temporary password comes first. The API refuses other staff calls until then anyway.
	if (me.must_change_password && url.pathname !== '/staff/password') redirect(307, '/staff/password');
	return { me };
};
```

In `frontend/src/routes/+layout.svelte`:
- Replace `<p class="menu-email">{me.email}</p>` with `<p class="menu-username">{me.username}</p>`, and rename the `.menu-email` CSS rule to `.menu-username`.
- Add this link right before the sign-out `Button`:

```svelte
					<a href="/staff/password" onclick={() => menu?.hidePopover()}>{m.password_heading()}</a>
```

- [ ] **Step 6: Change password page**

Create `frontend/src/routes/staff/password/+page.svelte`:

```svelte
<!-- Change password (T2.15, FR-A8, FR-A9). Forced after a temporary password: staff/+layout.ts sends staff here until
     it is done, and the API refuses other staff calls meanwhile. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError, changePassword } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';

	let { data } = $props();

	let current = $state('');
	let next = $state('');
	let repeat = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let busy = $state(false);

	// API field codes, translated here (CLAUDE.md "i18n").
	const fieldText: Record<string, () => string> = {
		required: m.err_required,
		wrong: m.password_err_wrong,
		too_short: m.password_err_too_short,
		too_long: m.password_err_too_long,
		same: m.password_err_same,
		mismatch: m.password_err_mismatch
	};
	const fieldError = (f: string) => (errors[f] ? (fieldText[errors[f]] ?? m.err_required)() : undefined);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = {};
		if (!current) errors.current_password = 'required';
		if (!next) errors.new_password = 'required';
		else if (next !== repeat) errors.repeat = 'mismatch';
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			await changePassword(current, next);
			showToast(m.password_changed());
			await goto('/staff', { invalidateAll: true });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') errors = err.fields;
			else if (err instanceof ApiError && err.code === 'auth.too_many_attempts') formError = m.login_err_too_many();
			else if (err instanceof ApiError && err.status === 401) await goto('/login');
			else formError = err instanceof ApiError ? m.detail_err_failed() : m.err_network();
		} finally {
			busy = false;
		}
	}
</script>

{#if !data.me.must_change_password}<a class="back" href="/staff">{m.detail_back()}</a>{/if}

<form onsubmit={submit} novalidate>
	<h1>{m.password_heading()}</h1>
	{#if data.me.must_change_password}<Alert>{m.password_forced()}</Alert>{/if}
	{#if formError}<Alert variant="error">{formError}</Alert>{/if}
	<!-- Lets password managers file the new password under the right account. -->
	<input type="text" autocomplete="username" value={data.me.username} hidden readonly />
	<TextInput
		label={m.password_current()}
		type="password"
		bind:value={current}
		error={fieldError('current_password')}
		autocomplete="current-password"
		required
	/>
	<TextInput
		label={m.password_new()}
		type="password"
		bind:value={next}
		error={fieldError('new_password')}
		helper={m.password_new_helper()}
		autocomplete="new-password"
		required
	/>
	<TextInput
		label={m.password_repeat()}
		type="password"
		bind:value={repeat}
		error={fieldError('repeat')}
		autocomplete="new-password"
		required
	/>
	<Button type="submit" disabled={busy}>{m.password_save()}</Button>
</form>

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		max-width: 440px;
		margin: var(--space-xl) auto 0;
		padding: var(--space-xl);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
	}
	h1 {
		margin: 0;
	}
</style>
```

- [ ] **Step 7: Admin staff page**

In `frontend/src/routes/staff/admin/staff/+page.svelte`:

Script: add `resetStaffPassword` to the `$lib/api` import. Replace the `fieldText`/`describeError` block and the create state and function with:

```ts
	// API error codes, translated here (CLAUDE.md "i18n"). Field codes cover name, username, email, password and role_id.
	const errorText: Record<string, () => string> = {
		'staff.root_admin_only': m.staff_err_root_admin_only,
		'staff.last_root_admin': m.staff_err_last_root_admin,
		'staff.not_found': m.staff_err_not_found,
		'auth.forbidden': m.detail_err_forbidden,
		'auth.required': m.detail_err_session,
		validation: m.detail_err_invalid
	};
	const fieldText: Record<string, () => string> = {
		required: m.err_required,
		too_long: m.err_too_long,
		too_short: m.password_err_too_short,
		invalid_characters: m.err_invalid_characters,
		not_found: m.staff_err_role_not_found
	};
	// Codes whose message depends on the field.
	const perField: Record<string, Record<string, () => string>> = {
		username: { invalid: m.staff_err_username_invalid, taken: m.staff_err_username_taken },
		email: { invalid: m.staff_err_email_invalid, taken: m.staff_err_email_taken },
		password: { too_long: m.password_err_too_long }
	};
	const codeText = (field: string, code: string) => (perField[field]?.[code] ?? fieldText[code] ?? m.err_required)();
	function describeError(err: unknown) {
		if (!(err instanceof ApiError)) return m.err_network();
		if (err.fields.role_id) return codeText('role_id', err.fields.role_id);
		if (err.fields.password) return codeText('password', err.fields.password);
		return (errorText[err.code] ?? m.detail_err_failed)();
	}

	let busy = $state(false);

	// Create (staff.create, Root Admin only: FR-A1). The password is temporary (FR-A8).
	let name = $state('');
	let username = $state('');
	let email = $state('');
	let roleId = $state('');
	let password = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	const fieldError = (f: string) => (errors[f] ? codeText(f, errors[f]) : undefined);

	async function create(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = {};
		if (!name.trim()) errors.name = 'required';
		if (!username.trim()) errors.username = 'required';
		if (!roleId) errors.role_id = 'required';
		if (!password) errors.password = 'required';
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			await createStaffAccount({ name, username, email, role_id: Number(roleId), password });
			name = username = email = roleId = password = '';
			showToast(m.staff_added());
			await invalidateAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') errors = err.fields;
			else if (err instanceof ApiError && err.code === 'staff.username_taken') errors = { username: 'taken' };
			else if (err instanceof ApiError && err.code === 'staff.email_taken') errors = { email: 'taken' };
			else formError = describeError(err);
		} finally {
			busy = false;
		}
	}
```

After `confirmActive`, add the reset flow:

```ts
	// Reset password (Root Admin only: FR-A9), in its own modal dialog.
	let resetDialog: HTMLDialogElement;
	let resetting = $state.raw<StaffAccount | null>(null);
	let resetPassword = $state('');
	let resetError = $state<string>();
	function askReset(a: StaffAccount) {
		resetting = a;
		resetPassword = '';
		resetError = undefined;
		resetDialog.showModal();
	}
	async function confirmReset(event: SubmitEvent) {
		event.preventDefault();
		if (!resetting) return;
		if (!resetPassword) return (resetError = m.err_required());
		busy = true;
		try {
			await resetStaffPassword(resetting.id, resetPassword);
			resetDialog.close();
			kept = resetting.id;
			showToast(m.staff_reset_done());
		} catch (err) {
			resetError = describeError(err);
		} finally {
			busy = false;
		}
	}
```

Markup, create form `.fields`: replace the email `TextInput` with the following, and add the password field after the role `Select`:

```svelte
						<TextInput label={m.staff_name()} bind:value={name} error={fieldError('name')} maxlength={100} autocomplete="off" required />
						<TextInput
							label={m.staff_username()}
							helper={m.staff_username_helper()}
							bind:value={username}
							error={fieldError('username')}
							maxlength={64}
							autocomplete="off"
							autocapitalize="none"
							spellcheck="false"
							required
						/>
						<TextInput label={m.staff_email()} type="email" bind:value={email} error={fieldError('email')} maxlength={254} autocomplete="off" />
```

```svelte
						<!-- Shown in clear on purpose: the Root Admin passes it on (FR-A8 makes the person replace it). -->
						<TextInput
							label={m.staff_temp_password()}
							helper={m.staff_temp_password_helper()}
							bind:value={password}
							error={fieldError('password')}
							autocomplete="off"
							spellcheck="false"
							required
						/>
```

Add a snippet after `toggle`:

```svelte
{#snippet resetButton(a: StaffAccount)}
	{#if can('staff.create')}
		<Button variant="secondary" disabled={busy} onclick={() => askReset(a)}>{m.staff_reset_password()}</Button>
	{/if}
{/snippet}
```

Table changes:
- Header: after the Name `th`, add `<th scope="col">{m.staff_username()}</th>`.
- Row: after `who`, add `<td>{a.username}</td>`, and change the email cell to `<td class="email">{#if a.email}{a.email}{:else}<span class="muted">{m.staff_no_email()}</span>{/if}</td>`.
- In the access cell, after `{#if editable(a)}{@render toggle(a)}{/if}`, add `{@render resetButton(a)}`.
- `colspan="5"` becomes `colspan="6"`.

Cards:
- Replace `<div class="muted email">{a.email}</div>` with `<div class="muted email">{a.username} · {a.email || m.staff_no_email()}</div>`.
- After the `{#if editable(a)}...{/if}` block, add `<div>{@render resetButton(a)}</div>`.

After the existing `</dialog>`, add:

```svelte
<dialog bind:this={resetDialog} aria-labelledby="reset-title">
	{#if resetting}
		<h2 id="reset-title">{m.staff_reset_title({ name: resetting.name })}</h2>
		<p>{m.staff_reset_text()}</p>
		<form class="reset" onsubmit={confirmReset} novalidate>
			<TextInput
				label={m.staff_temp_password()}
				helper={m.staff_temp_password_helper()}
				bind:value={resetPassword}
				error={resetError}
				autocomplete="off"
				spellcheck="false"
				required
			/>
			<Button type="button" variant="secondary" onclick={() => resetDialog.close()}>{m.admin_cancel()}</Button>
			<Button type="submit" disabled={busy}>{m.staff_reset_password()}</Button>
		</form>
	{/if}
</dialog>
```

CSS, after the `dialog form` rule:

```css
	dialog form.reset :global(.field) {
		flex-basis: 100%;
	}
```

- [ ] **Step 8: Run the checks and e2e, and confirm they pass**

Run: `cd frontend && npm run check`
Expected: 0 errors.

Stop any running Go API, then run: `npm run test:e2e`
Expected: all pass, with axe clean on the forced password page and the admin pages at both sizes.

Accessibility spot check: `npm run dev`, open http://localhost:5173/login and /staff/password with the axe gallery method from CLAUDE.md. Expected: no violations.

- [ ] **Step 9: Translations**

Dispatch the `i18n-translator` agent. Give it the list of keys added or changed in Step 2 (not the deleted one) and ask it to translate them in zh-CN, my and th.

Then run `node scripts/check-i18n.mjs`.
Expected: PASS.

---

### Task 8: Docs, review and close

**Files:**
- Modify: `CLAUDE.md` (Commands), `.claude/rules/frontend.md`, `docs/PLAN.md` (T2.15 status, T3.10 note), `DESIGN.md` ("Staff accounts"), `PROGRESS.md` (via `/progress`)

- [ ] **Step 1: CLAUDE.md**

In the Commands section:
- "Run locally": `make dev` (Go API on :8080, SvelteKit on http://localhost:5173, which proxies `/api` and `/healthz`).
- Replace the "Staff sign-in in dev" line with: open /login and sign in as `root`, `agent` or `viewer` with password `dev-password` (from `make seed`).
- "First Root Admin": `ticket-app create-root-admin --username <username> [--email <email>]`. It prints a temporary password once, which must be changed at first sign-in (FR-A6).
- End-to-end line: "starts the Go API and Vite if they are not running".

- [ ] **Step 2: Rules and plan notes**

- `.claude/rules/frontend.md`: drop the IdP example from the last bullet, leaving "Links and GET forms that must leave the SPA need `data-sveltekit-reload`; otherwise the SvelteKit router handles them and shows its 404 page."
- `docs/PLAN.md`, task **T3.10**, add: "Login limit (FR-A11) and audit IPs use `clientIP`: pass the real client IP from NGINX (trusted proxy, `X-Forwarded-For`) or the per-IP limit counts every staff member as one IP."

- [ ] **Step 3: Full checks**

Run `/check`.
Expected: Go tests, lint, svelte-check and i18n all pass. Fix every failure first.

- [ ] **Step 4: Security review**

Dispatch the `security-reviewer` agent with a 300-word limit and these four files:
- `backend/internal/api/auth.go`
- `backend/internal/auth/credentials.go`
- `backend/internal/auth/limit.go`
- `backend/internal/api/staff.go` (`createStaff`, `resetPassword`)

Checklist: timing and enumeration (FR-A2), login CSRF, rate limit bypass, session revocation (FR-A10), temporary-password gate (FR-A8), audit without secrets, Root Admin-only reset (FR-A9).

Fix its findings, then go back to Step 3. If it stalls twice, review inline against the same checklist and record that in PROGRESS.md.

- [ ] **Step 5: Confirm the done criteria with evidence**

Evidence comes from command output only:
- `make test-go PKG=./internal/api RUN='Login_Refused|TemporaryPassword|ResetPassword|UsernameLimit'` PASS (FR-A2, FR-A8, FR-A10, FR-A11).
- `make test-go PKG=./cmd/ticket-app RUN=RootAdmin` PASS (FR-A6).
- `npm run test:e2e` PASS (sign-in and forced change).
- `grep -rIli oidc backend frontend Makefile --exclude-dir=node_modules --exclude-dir=bin --exclude-dir=.svelte-kit --exclude-dir=build --exclude-dir=dist` prints nothing.
- The security-reviewer has no open findings.

- [ ] **Step 6: Close**

Tick T2.15 in docs/PLAN.md, then run `/progress`. It writes docs/notes/T2.15.md, updates the stale docs (DESIGN.md "Staff accounts" gets the username column, the optional email and the Root Admin-only "Reset password" action with its dialog; ARCHITECTURE.md is already updated) and updates PROGRESS.md.
