package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

func accountPath(id int64) string { return "/api/staff/accounts/" + strconv.FormatInt(id, 10) }

func (e *testEnv) roleID(name string) int64 {
	e.t.Helper()
	var id int64
	if err := e.db.Raw("SELECT id FROM roles WHERE name = ?", name).Scan(&id).Error; err != nil || id == 0 {
		e.t.Fatalf("role %q: %v", name, err)
	}
	return id
}

// errorBody is the error code of an API error and, for "validation", the code per field.
func errorBody(rec *httptest.ResponseRecorder) (code string, fields map[string]string) {
	var b struct {
		Error  string
		Fields map[string]string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return b.Error, b.Fields
}

// isolated moves the rest of the test into a transaction that Cleanup rolls back. e.db and the server both
// use it, so the test can deactivate shared rows such as root@dev.test without other tests or packages seeing it.
func (e *testEnv) isolated() {
	e.t.Helper()
	tx := e.db.Begin()
	if tx.Error != nil {
		e.t.Fatal(tx.Error)
	}
	e.t.Cleanup(func() { tx.Rollback() })
	e.db = tx
	e.mux = http.NewServeMux()
	(&Server{DB: tx, UploadDir: e.dir, Sessions: e.sessions, GuestTicketLimit: 1000}).Routes(e.mux)
}

// staffAudits lists the audit rows about target written after row since, as "action from>to".
func (e *testEnv) staffAudits(target string, since int64) []string {
	e.t.Helper()
	var rows []models.AuditEntry
	if err := e.db.Where("target = ? AND id > ?", target, since).Order("id").Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, a := range rows {
		s := a.Action
		if a.FromValue != nil || a.ToValue != nil {
			s += " " + optText(a.FromValue) + ">" + optText(a.ToValue)
		}
		out = append(out, s)
	}
	return out
}

func (e *testEnv) staffRow(id int64) models.Staff {
	e.t.Helper()
	var st models.Staff
	if err := e.db.First(&st, id).Error; err != nil {
		e.t.Fatal(err)
	}
	return st
}

// FR-A1: staff.manage lists every account with its role; roles without it get 403.
func TestStaffList_FRA1(t *testing.T) {
	e := newAuthEnv(t)
	off := e.newStaff("Agent", false)
	admin, lead := e.session(e.newStaff("Admin", true)), e.session(e.newStaff("Team Lead", true))

	rec := e.withCookie(http.MethodGet, "/api/staff/accounts", admin)
	var list []staffAccount
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	i := slices.IndexFunc(list, func(a staffAccount) bool { return a.ID == off.ID })
	if i < 0 {
		t.Fatalf("inactive Agent %d not listed", off.ID)
	}
	if a := list[i]; a.Username != off.Username || a.Role != (namedItem{off.RoleID, "Agent"}) || a.IsActive || a.CreatedAt.IsZero() {
		t.Errorf("inactive Agent in list = %+v", a)
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/accounts", lead); rec.Code != http.StatusForbidden {
		t.Errorf("Team Lead GET = %d, want 403", rec.Code)
	}
}

// FR-A1: only Root Admin creates staff accounts (name, username, temporary password, role; no email). Input is
// checked per field; the username is stored lower case and unique ignoring case; the password is stored only as a hash and must be changed at first sign-in (FR-A8); staff.created is audited.
func TestStaffCreate_FRA1(t *testing.T) {
	e := newAuthEnv(t)
	rootSt := e.newStaff("Root Admin", true)
	root, admin, lead := e.session(rootSt), e.session(e.newStaff("Admin", true)), e.session(e.newStaff("Team Lead", true))
	taken := e.newStaff("Agent", true)
	agent := e.roleID("Agent")
	seq := 0 // the clock can return the same nanosecond for back-to-back calls
	user := func() string { seq++; return fmt.Sprintf("New.%d-%d", time.Now().UnixNano(), seq) }
	in := func(name, username string, role int64, password string) map[string]any {
		return map[string]any{"name": name, "username": username, "role_id": role, "password": password}
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
		{"Root Admin creates", root, in(" New Agent ", " "+user()+" ", agent, pw), 201, "", nil},
		{"Admin refused", admin, in("New Agent", user(), agent, pw), 403, "auth.forbidden", nil},
		{"Team Lead refused", lead, in("New Agent", user(), agent, pw), 403, "auth.forbidden", nil},
		{"username with space", root, in("X", "new agent", agent, pw), 400, "validation", map[string]string{"username": "invalid"}},
		{"username too short", root, in("X", "ab", agent, pw), 400, "validation", map[string]string{"username": "invalid"}},
		{"username missing", root, in("X", "  ", agent, pw), 400, "validation", map[string]string{"username": "required"}},
		{"username taken in other case", root, in("X", strings.ToUpper(taken.Username), agent, pw), 409, "staff.username_taken", nil},
		{"password too short", root, in("X", user(), agent, "short"), 400, "validation", map[string]string{"password": "too_short"}},
		{"password missing", root, in("X", user(), agent, ""), 400, "validation", map[string]string{"password": "required"}},
		{"role unknown", root, in("X", user(), 999999999, pw), 400, "validation", map[string]string{"role_id": "not_found"}},
		{"name blank", root, in("  ", user(), agent, pw), 400, "validation", map[string]string{"name": "required"}},
		{"name too long", root, in(strings.Repeat("a", 101), user(), agent, pw), 400, "validation", map[string]string{"name": "too_long"}},
		{"all fields bad", root, in("", "", 0, ""), 400, "validation",
			map[string]string{"name": "required", "username": "required", "password": "required", "role_id": "not_found"}},
		{"email field", root, map[string]any{"name": "X", "username": user(), "email": "x@test.local", "role_id": agent, "password": pw}, 400, "invalid_body", nil},
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
			if got.ID == 0 || got.Name != "New Agent" || got.Username != username ||
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

// FR-A3: an Admin cannot get staff.create or role.manage through the staff API. Creating accounts is refused,
// taking or handing out the Root Admin role is refused, and there is no field for permissions.
func TestStaffRootOnly_FRA3(t *testing.T) {
	e := newAuthEnv(t)
	admin, other := e.newStaff("Admin", true), e.newStaff("Admin", true)
	c := e.session(admin)
	rootRole := fmt.Sprintf(`{"role_id": %d}`, e.roleID("Root Admin"))

	tests := []struct {
		name, method, path, body string
		code                     int
		err                      string
	}{
		{"create a Root Admin", http.MethodPost, "/api/staff/accounts",
			fmt.Sprintf(`{"name": "X", "username": "x%d", "role_id": %d, "password": %q}`, time.Now().UnixNano(), e.roleID("Root Admin"), testPassword), 403, "auth.forbidden"},
		{"take Root Admin", http.MethodPatch, accountPath(admin.ID), rootRole, 403, "staff.root_admin_only"},
		{"give Root Admin", http.MethodPatch, accountPath(other.ID), rootRole, 403, "staff.root_admin_only"},
		{"add permissions", http.MethodPatch, accountPath(admin.ID), `{"permissions": ["staff.create", "role.manage"]}`, 400, "invalid_body"},
	}
	for _, tt := range tests {
		rec := e.sendJSON(tt.method, tt.path, c, tt.body)
		if code, _ := errorBody(rec); rec.Code != tt.code || code != tt.err {
			t.Errorf("%s: %s = %d %s, want %d %s", tt.name, tt.method, rec.Code, rec.Body, tt.code, tt.err)
		}
	}
	var me meOutput
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); json.Unmarshal(rec.Body.Bytes(), &me) != nil {
		t.Fatalf("me = %d %s", rec.Code, rec.Body)
	}
	if me.Role != "Admin" || slices.Contains(me.Permissions, rbac.StaffCreate) || slices.Contains(me.Permissions, rbac.RoleManage) {
		t.Errorf("Admin after attempts = %s %v", me.Role, me.Permissions)
	}
	if st := e.staffRow(other.ID); st.RoleID != other.RoleID {
		t.Errorf("other Admin's role changed to %d", st.RoleID)
	}
}

// FR-A4: an Admin can neither assign the Root Admin role nor demote or deactivate a Root Admin; a Root Admin can.
// Changes are audited with old and new role; a field set to its current value changes and audits nothing.
func TestStaffRole_FRA4(t *testing.T) {
	e := newAuthEnv(t)
	admin, root := e.session(e.newStaff("Admin", true)), e.session(e.newStaff("Root Admin", true))
	role := func(name string) string { return fmt.Sprintf(`{"role_id": %d}`, e.roleID(name)) }

	tests := []struct {
		name   string
		c      *http.Cookie
		target string // role of the staff member changed; "" = an ID nobody has
		off    bool   // target is inactive
		body   string
		code   int
		err    string
		audits []string
	}{
		{"Admin grants Root Admin", admin, "Agent", false, role("Root Admin"), 403, "staff.root_admin_only", nil},
		{"Admin demotes Root Admin", admin, "Root Admin", false, role("Agent"), 403, "staff.root_admin_only", nil},
		{"Admin deactivates Root Admin", admin, "Root Admin", false, `{"is_active": false}`, 403, "staff.root_admin_only", nil},
		{"Admin reactivates Root Admin", admin, "Root Admin", true, `{"is_active": true}`, 403, "staff.root_admin_only", nil},
		{"Admin sets Team Lead", admin, "Agent", false, role("Team Lead"), 200, "", []string{"staff.role_changed Agent>Team Lead"}},
		{"Admin deactivates Agent", admin, "Agent", false, `{"is_active": false}`, 200, "", []string{"staff.deactivated"}},
		{"Admin reactivates Agent", admin, "Agent", true, `{"is_active": true}`, 200, "", []string{"staff.reactivated"}},
		{"Admin changes both", admin, "Agent", false, fmt.Sprintf(`{"role_id": %d, "is_active": false}`, e.roleID("Viewer")), 200, "",
			[]string{"staff.role_changed Agent>Viewer", "staff.deactivated"}},
		{"nothing changes", admin, "Agent", false, fmt.Sprintf(`{"role_id": %d, "is_active": true}`, e.roleID("Agent")), 200, "", nil},
		{"Root Admin grants Root Admin", root, "Agent", false, role("Root Admin"), 200, "", []string{"staff.role_changed Agent>Root Admin"}},
		{"Root Admin demotes Root Admin", root, "Root Admin", false, role("Admin"), 200, "", []string{"staff.role_changed Root Admin>Admin"}},
		{"role unknown", admin, "Agent", false, `{"role_id": 999999999}`, 400, "validation", nil},
		{"empty body", admin, "Agent", false, `{}`, 400, "invalid_body", nil},
		{"staff unknown", admin, "", false, `{"is_active": false}`, 404, "staff.not_found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			var st models.Staff
			if tt.target != "" {
				st = e.newStaff(tt.target, !tt.off)
			}
			since := e.lastAuditID()
			rec := e.sendJSON(http.MethodPatch, accountPath(st.ID), tt.c, tt.body)
			if code, _ := errorBody(rec); rec.Code != tt.code || code != tt.err {
				t.Fatalf("PATCH = %d %s, want %d %s", rec.Code, rec.Body, tt.code, tt.err)
			}
			if st.ID == 0 {
				return
			}
			if a := e.staffAudits(st.Username, since); !slices.Equal(a, tt.audits) {
				t.Errorf("audits = %v, want %v", a, tt.audits)
			}
			now := e.staffRow(st.ID)
			if tt.code != http.StatusOK {
				if now.RoleID != st.RoleID || now.IsActive != st.IsActive {
					t.Errorf("refused request changed the account: role %d active %v", now.RoleID, now.IsActive)
				}
				return
			}
			var got staffAccount
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != st.ID || got.Role.ID != now.RoleID || got.IsActive != now.IsActive {
				t.Errorf("response %+v does not match the account: role %d active %v", got, now.RoleID, now.IsActive)
			}
		})
	}
}

// FR-A5: the last active Root Admin cannot demote or deactivate themselves; with a second one they can.
func TestLastRootAdmin_FRA5(t *testing.T) {
	tests := []struct {
		name               string
		roots              int // active Root Admins
		demote, deactivate bool
		code               int
	}{
		{"last demotes self", 1, true, false, 409},
		{"last deactivates self", 1, false, true, 409},
		{"last does both", 1, true, true, 409},
		{"one of two demotes self", 2, true, false, 200},
		{"one of two deactivates self", 2, false, true, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAuthEnv(t)
			e.isolated()
			// Inside the transaction only: root@dev.test stays active for everyone else.
			if err := e.db.Exec(`UPDATE staff SET is_active = false WHERE is_active AND role_id IN
				(SELECT role_id FROM role_permissions WHERE permission = ?)`, rbac.RoleManage).Error; err != nil {
				t.Fatal(err)
			}
			me := e.newStaff("Root Admin", true)
			for range tt.roots - 1 {
				e.newStaff("Root Admin", true)
			}
			in := map[string]any{}
			if tt.demote {
				in["role_id"] = e.roleID("Admin")
			}
			if tt.deactivate {
				in["is_active"] = false
			}
			body, _ := json.Marshal(in)

			rec := e.sendJSON(http.MethodPatch, accountPath(me.ID), e.session(me), string(body))
			if rec.Code != tt.code {
				t.Fatalf("PATCH = %d %s, want %d", rec.Code, rec.Body, tt.code)
			}
			now := e.staffRow(me.ID)
			if changed := now.RoleID != me.RoleID || !now.IsActive; changed != (tt.code == http.StatusOK) {
				t.Errorf("after %d: role %d active %v", rec.Code, now.RoleID, now.IsActive)
			}
			if code, _ := errorBody(rec); tt.code == http.StatusConflict && code != "staff.last_root_admin" {
				t.Errorf("error = %q, want staff.last_root_admin", code)
			}
		})
	}
}

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
