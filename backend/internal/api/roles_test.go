package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

func rolePermsPath(id int64) string {
	return "/api/staff/roles/" + strconv.FormatInt(id, 10) + "/permissions"
}

// rolePerms is a role's permissions in the DB, sorted.
func (e *testEnv) rolePerms(id int64) []string {
	e.t.Helper()
	var out []string
	if err := e.db.Model(&models.RolePermission{}).Where("role_id = ?", id).Order("permission").Pluck("permission", &out).Error; err != nil {
		e.t.Fatal(err)
	}
	return out
}

// setRolePerms replaces a role's permissions directly in the DB.
func (e *testEnv) setRolePerms(id int64, perms []string) {
	e.t.Helper()
	if err := e.db.Exec("DELETE FROM role_permissions WHERE role_id = ?", id).Error; err != nil {
		e.t.Fatal(err)
	}
	for _, p := range perms {
		if err := e.db.Create(&models.RolePermission{RoleID: id, Permission: p}).Error; err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *testEnv) mePermissions(c *http.Cookie) []string {
	e.t.Helper()
	var me meOutput
	if rec := e.withCookie(http.MethodGet, "/api/auth/me", c); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &me) != nil {
		e.t.Fatalf("me = %d %s", rec.Code, rec.Body)
	}
	return me.Permissions
}

// FR-A3 (T2.09 done criterion): an Admin (staff.manage, no role.manage) cannot raise their own access. Creating a
// role, editing their own role or the Root Admin role, and taking the Root Admin role are refused. Afterwards the
// Admin's permissions, account and both roles are unchanged, and nothing is audited.
func TestAdminCannotRaiseOwnAccess_FRA3(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated() // a regression that lets the Admin through is rolled back
	admin := e.newStaff("Admin", true)
	c := e.session(admin)
	rootID := e.roleID("Root Admin")
	before, adminRole, rootRole := e.mePermissions(c), e.rolePerms(admin.RoleID), e.rolePerms(rootID)
	all, _ := json.Marshal(map[string]any{"permissions": rbac.All})
	since := e.lastAuditID()

	tests := []struct {
		name, method, path, body string
		err                      string
	}{
		{"create a role", http.MethodPost, "/api/staff/roles",
			fmt.Sprintf(`{"name": "Mine %d", "permissions": ["staff.create", "role.manage"]}`, time.Now().UnixNano()), "auth.forbidden"},
		{"edit own role", http.MethodPut, rolePermsPath(admin.RoleID), string(all), "auth.forbidden"},
		{"edit Root Admin role", http.MethodPut, rolePermsPath(rootID), `{"permissions": ["ticket.view_all"]}`, "auth.forbidden"},
		{"take Root Admin role", http.MethodPatch, accountPath(admin.ID), fmt.Sprintf(`{"role_id": %d}`, rootID), "staff.root_admin_only"},
	}
	for _, tt := range tests {
		rec := e.sendJSON(tt.method, tt.path, c, tt.body)
		if code, _ := errorBody(rec); rec.Code != http.StatusForbidden || code != tt.err {
			t.Errorf("%s: %s %s = %d %s, want 403 %s", tt.name, tt.method, tt.path, rec.Code, rec.Body, tt.err)
		}
	}

	if got := e.mePermissions(c); !slices.Equal(got, before) || slices.Contains(got, rbac.StaffCreate) || slices.Contains(got, rbac.RoleManage) {
		t.Errorf("Admin permissions = %v, before %v", got, before)
	}
	if got := e.rolePerms(admin.RoleID); !slices.Equal(got, adminRole) {
		t.Errorf("Admin role = %v, was %v", got, adminRole)
	}
	if got := e.rolePerms(rootID); !slices.Equal(got, rootRole) {
		t.Errorf("Root Admin role = %v, was %v", got, rootRole)
	}
	if st := e.staffRow(admin.ID); st.RoleID != admin.RoleID {
		t.Errorf("Admin's role changed to %d", st.RoleID)
	}
	var n int64
	e.db.Model(&models.AuditEntry{}).Where("id > ? AND actor_staff_id = ?", since, admin.ID).Count(&n)
	if n != 0 {
		t.Errorf("%d audit rows by the Admin, want none", n)
	}
}

// FR-R2, FR-A3, FR-L1: Root Admin creates a role with a name and permissions. The name is unique ignoring case,
// permissions come from the fixed list, staff.create and role.manage are refused, and role.created is audited.
func TestRoleCreate_FRA3(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated() // every role created here is rolled back
	rootSt := e.newStaff("Root Admin", true)
	root := e.session(rootSt)
	n := 0 // the Windows clock can return one UnixNano twice
	uniq := func() string { n++; return fmt.Sprintf("Role %d-%d", time.Now().UnixNano(), n) }
	in := func(name string, perms ...string) map[string]any {
		return map[string]any{"name": name, "permissions": perms}
	}

	tests := []struct {
		name   string
		in     map[string]any
		code   int
		err    string
		fields map[string]string
		perms  []string // the created role's permissions
	}{
		{"creates", in(" "+uniq()+" ", "ticket.update", "ticket.view_all", "ticket.update"), 201, "", nil, []string{"ticket.update", "ticket.view_all"}},
		{"no permissions", in(uniq()), 201, "", nil, []string{}},
		{"name of 50 characters", in(strings.Repeat(string(rune(0x0E23)), 50), "report.view"), 201, "", nil, []string{"report.view"}},
		{"staff.create", in(uniq(), "ticket.view_all", "staff.create"), 400, "validation", map[string]string{"permissions": "root_only"}, nil},
		{"role.manage", in(uniq(), "role.manage"), 400, "validation", map[string]string{"permissions": "root_only"}, nil},
		{"unknown permission", in(uniq(), "ticket.view_all", "ticket.fly"), 400, "validation", map[string]string{"permissions": "invalid"}, nil},
		{"unknown and root-only", in(uniq(), "role.manage", "admin"), 400, "validation", map[string]string{"permissions": "invalid"}, nil},
		{"name taken in other case", in("aDMIN", "ticket.view_all"), 409, "role.name_taken", nil, nil},
		{"name blank", in("  "), 400, "validation", map[string]string{"name": "required"}, nil},
		{"name too long", in(strings.Repeat("a", 51)), 400, "validation", map[string]string{"name": "too_long"}, nil},
		{"name with control character", in("Desk" + string(rune(0x07))), 400, "validation", map[string]string{"name": "invalid_characters"}, nil},
		{"all fields bad", in("", "staff.create"), 400, "validation", map[string]string{"name": "required", "permissions": "root_only"}, nil},
		{"unknown field", map[string]any{"name": uniq(), "root": true}, 400, "invalid_body", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			name := strings.TrimSpace(tt.in["name"].(string))
			count := func() (n int64) {
				e.db.Table("roles").Where("lower(name) = lower(?)", name).Count(&n)
				return n
			}
			before, since := count(), e.lastAuditID()
			body, _ := json.Marshal(tt.in)
			rec := e.sendJSON(http.MethodPost, "/api/staff/roles", root, string(body))
			code, fields := errorBody(rec)
			if rec.Code != tt.code || code != tt.err || !maps.Equal(fields, tt.fields) {
				t.Fatalf("POST = %d %s, want %d %s %v", rec.Code, rec.Body, tt.code, tt.err, tt.fields)
			}
			if tt.code != http.StatusCreated {
				if n := count(); n != before {
					t.Errorf("refused request changed roles named %q: %d rows, was %d", name, n, before)
				}
				if a := e.staffAudits(name, since); a != nil {
					t.Errorf("refused request audited %v", a)
				}
				return
			}
			var got roleOutput
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.ID == 0 || got.Name != name || got.Root || got.StaffCount != 0 || got.Permissions == nil || !slices.Equal(got.Permissions, tt.perms) {
				t.Errorf("created = %+v, want %q with %v", got, name, tt.perms)
			}
			if p := e.rolePerms(got.ID); !slices.Equal(p, tt.perms) {
				t.Errorf("stored permissions = %v, want %v", p, tt.perms)
			}
			want := "role.created"
			if len(tt.perms) > 0 {
				want += " >" + strings.Join(tt.perms, ",")
			}
			if a := e.staffAudits(name, since); !slices.Equal(a, []string{want}) {
				t.Errorf("audits = %v, want %s", a, want)
			}
			if actor, id, _ := e.auditRow("role.created", name); actor != "staff" || id == nil || *id != rootSt.ID {
				t.Errorf("role.created actor = %s %v, want the Root Admin", actor, id)
			}
		})
	}
}

// FR-R2, FR-A3, FR-L1: Root Admin replaces a role's permissions. role.changed records the removed permissions as
// from and the added ones as to; no change writes and audits nothing. staff.create and role.manage are refused, and
// the Root Admin role cannot be edited at all, so nobody can take role.manage off it and lock everyone out.
func TestRolePermissions_FRA3(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	root := e.session(e.newStaff("Root Admin", true))
	id, name := e.newRole()
	rootID := e.roleID("Root Admin")
	rootPerms := e.rolePerms(rootID)
	start := []string{"report.view", "ticket.view_all"}
	body := func(perms ...string) string {
		b, _ := json.Marshal(map[string]any{"permissions": perms})
		return string(b)
	}

	tests := []struct {
		name   string
		role   int64 // 0 = the test role
		body   string
		code   int
		err    string
		fields map[string]string
		want   []string // the test role's permissions afterwards
		audits []string
	}{
		{"add and remove", 0, body("ticket.view_all", "ticket.update", "ticket.comment", "ticket.update"), 200, "", nil,
			[]string{"ticket.comment", "ticket.update", "ticket.view_all"}, []string{"role.changed report.view>ticket.comment,ticket.update"}},
		{"add only", 0, body("report.view", "ticket.view_all", "audit.view"), 200, "", nil,
			[]string{"audit.view", "report.view", "ticket.view_all"}, []string{"role.changed >audit.view"}},
		{"remove all", 0, `{"permissions": []}`, 200, "", nil, nil, []string{"role.changed report.view,ticket.view_all>"}},
		{"same set", 0, body("ticket.view_all", "report.view"), 200, "", nil, start, nil},
		{"add staff.create", 0, body("report.view", "ticket.view_all", "staff.create"), 400, "validation", map[string]string{"permissions": "root_only"}, start, nil},
		{"add role.manage", 0, body("role.manage"), 400, "validation", map[string]string{"permissions": "root_only"}, start, nil},
		{"unknown permission", 0, body("ticket.view_all", "Ticket.View_All"), 400, "validation", map[string]string{"permissions": "invalid"}, start, nil},
		{"permissions missing", 0, `{}`, 400, "invalid_body", nil, start, nil},
		{"Root Admin role", rootID, body("ticket.view_all"), 403, "role.root_fixed", nil, start, nil},
		{"Root Admin role, same set", rootID, body(rootPerms...), 403, "role.root_fixed", nil, start, nil},
		{"unknown role", 999999999, body("ticket.view_all"), 404, "role.not_found", nil, start, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			e.setRolePerms(id, start)
			target := tt.role
			if target == 0 {
				target = id
			}
			since := e.lastAuditID()
			rec := e.sendJSON(http.MethodPut, rolePermsPath(target), root, tt.body)
			code, fields := errorBody(rec)
			if rec.Code != tt.code || code != tt.err || !maps.Equal(fields, tt.fields) {
				t.Fatalf("PUT = %d %s, want %d %s %v", rec.Code, rec.Body, tt.code, tt.err, tt.fields)
			}
			if got := e.rolePerms(id); !slices.Equal(got, tt.want) {
				t.Errorf("permissions = %v, want %v", got, tt.want)
			}
			if got := e.rolePerms(rootID); !slices.Equal(got, rootPerms) {
				t.Errorf("Root Admin permissions = %v, was %v", got, rootPerms)
			}
			if a := e.staffAudits(name, since); !slices.Equal(a, tt.audits) {
				t.Errorf("audits = %v, want %v", a, tt.audits)
			}
			var n int64
			e.db.Model(&models.AuditEntry{}).Where("id > ? AND action LIKE 'role.%'", since).Count(&n)
			if n != int64(len(tt.audits)) {
				t.Errorf("%d role audit rows, want %d", n, len(tt.audits))
			}
			if tt.code != http.StatusOK {
				return
			}
			var got roleOutput
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != id || got.Name != name || got.Root ||
				got.Permissions == nil || !slices.Equal(got.Permissions, tt.want) {
				t.Errorf("response = %s, want the role with %v", rec.Body, tt.want)
			}
		})
	}
}

// FR-R2: staff.manage lists every role with its sorted permissions and staff count, ordered by name, so an Admin
// can pick roles for staff. Roles without staff.manage get 403.
func TestRoleList_FRR2(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	id, name := e.newRole()
	e.setRolePerms(id, []string{"ticket.view_all", "report.view"})
	e.newStaff(name, true)
	e.newStaff(name, false)

	tests := []struct {
		role string
		code int
	}{
		{"Admin", 200},
		{"Root Admin", 200},
		{"Team Lead", 403},
		{"Agent", 403},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			e.t = t
			rec := e.withCookie(http.MethodGet, "/api/staff/roles", e.session(e.newStaff(tt.role, true)))
			if rec.Code != tt.code {
				t.Fatalf("GET = %d %s, want %d", rec.Code, rec.Body, tt.code)
			}
			if tt.code != http.StatusOK {
				return
			}
			var list []roleOutput
			if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
				t.Fatal(err)
			}
			at := func(n string) int {
				i := slices.IndexFunc(list, func(r roleOutput) bool { return r.Name == n })
				if i < 0 {
					t.Fatalf("role %q not listed", n)
				}
				return i
			}
			if r := list[at(name)]; r.ID != id || r.Root || r.StaffCount != 2 || !slices.Equal(r.Permissions, []string{"report.view", "ticket.view_all"}) {
				t.Errorf("test role = %+v", r)
			}
			all := slices.Sorted(slices.Values(rbac.All))
			if r := list[at("Root Admin")]; !r.Root || !slices.Equal(r.Permissions, all) || r.StaffCount < 1 {
				t.Errorf("Root Admin = %+v", r)
			}
			if r := list[at("Viewer")]; r.Root || r.Permissions == nil {
				t.Errorf("Viewer = %+v", r)
			}
			if order := []int{at("Admin"), at("Agent"), at("Root Admin"), at("Team Lead"), at("Viewer")}; !slices.IsSorted(order) {
				t.Errorf("default roles at %v, want ordered by name", order)
			}
		})
	}
}

// FR-R3: a permission change through the roles API applies to a signed-in member of the role on their next request.
func TestRoleChangeNextRequest_FRR3(t *testing.T) {
	e := newAuthEnv(t)
	e.isolated()
	root := e.session(e.newStaff("Root Admin", true))
	id, name := e.newRole()
	member := e.session(e.newStaff(name, true))

	steps := []struct {
		name, body string
		want       int
	}{
		{"no permissions", "", 403},
		{"staff.manage added", `{"permissions": ["staff.manage"]}`, 200},
		{"staff.manage removed", `{"permissions": ["ticket.view_all"]}`, 403},
	}
	for _, s := range steps {
		if s.body != "" {
			if rec := e.sendJSON(http.MethodPut, rolePermsPath(id), root, s.body); rec.Code != http.StatusOK {
				t.Fatalf("%s: PUT = %d %s", s.name, rec.Code, rec.Body)
			}
		}
		if rec := e.withCookie(http.MethodGet, "/api/staff/roles", member); rec.Code != s.want {
			t.Errorf("%s: member GET roles = %d, want %d", s.name, rec.Code, s.want)
		}
	}
}
