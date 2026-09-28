package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/rbac"
)

// requireMux replaces e.mux with GET /p/<permission> -> require(permission) -> 200, for every permission.
func (e *testEnv) requireMux() {
	s := &Server{DB: e.db, Sessions: e.sessions}
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	e.mux = http.NewServeMux()
	for _, p := range rbac.All {
		e.mux.HandleFunc("GET /p/"+p, s.require(p, ok))
	}
}

// FR-R2, FR-R3: each default role passes require() for exactly its permissions (table in docs/REQUIREMENTS.md).
func TestRequire_FRR2(t *testing.T) {
	e := newAuthEnv(t)
	e.requireMux()
	want := map[string][]string{ // same table as migrations.TestDefaultRoles_FRR2
		"Root Admin": rbac.All,
		"Admin": {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign", "ticket.delete",
			"report.view", "audit.view", "category.manage", "staff.manage"},
		"Team Lead": {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign",
			"report.view", "audit.view", "category.manage"},
		"Agent":  {"ticket.view_all", "ticket.comment", "ticket.update", "ticket.assign"},
		"Viewer": {"ticket.view_all", "report.view"},
	}
	for role, perms := range want {
		c := e.session(e.newStaff(role, true))
		for _, p := range rbac.All {
			t.Run(role+"/"+p, func(t *testing.T) {
				code := http.StatusForbidden
				if slices.Contains(perms, p) {
					code = http.StatusOK
				}
				if rec := e.withCookie(http.MethodGet, "/p/"+p, c); rec.Code != code {
					t.Errorf("GET = %d %s, want %d", rec.Code, rec.Body, code)
				}
			})
		}
	}
	if rec := e.withCookie(http.MethodGet, "/p/"+rbac.TicketViewAll, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no cookie = %d, want 401", rec.Code)
	}
}

// FR-R3: permission and role changes apply on the next request of an existing session.
func TestRequireRoleChange_FRR3(t *testing.T) {
	e := newAuthEnv(t)
	e.requireMux()
	roleID, name := e.newRole()
	var adminID int64
	if err := e.db.Raw("SELECT id FROM roles WHERE name = 'Admin'").Scan(&adminID).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Exec("INSERT INTO role_permissions VALUES (?, ?)", roleID, rbac.TicketUpdate).Error; err != nil {
		t.Fatal(err)
	}
	st := e.newStaff(name, true)
	c := e.session(st)
	path := "/p/" + rbac.TicketUpdate

	steps := []struct {
		name   string
		change string
		args   []any
		want   int
	}{
		{"granted", "", nil, http.StatusOK},
		{"permission removed", "DELETE FROM role_permissions WHERE role_id = ?", []any{roleID}, http.StatusForbidden},
		{"moved to Admin", "UPDATE staff SET role_id = ? WHERE id = ?", []any{adminID, st.ID}, http.StatusOK},
	}
	for _, s := range steps {
		if s.change != "" {
			if err := e.db.Exec(s.change, s.args...).Error; err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
		}
		if rec := e.withCookie(http.MethodGet, path, c); rec.Code != s.want {
			t.Errorf("%s: GET = %d %s, want %d", s.name, rec.Code, rec.Body, s.want)
		}
	}
}

// newRole adds a role with no permissions. Call it before newStaff: cleanups run last first, so this one
// runs after the staff cleanup, and moving staff off first covers a kept (deactivated) row.
func (e *testEnv) newRole() (id int64, name string) {
	e.t.Helper()
	name = fmt.Sprintf("Test role %d", time.Now().UnixNano())
	if err := e.db.Raw("INSERT INTO roles (name) VALUES (?) RETURNING id", name).Scan(&id).Error; err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() {
		e.db.Exec("UPDATE staff SET role_id = (SELECT id FROM roles WHERE name = 'Viewer') WHERE role_id = ?", id)
		e.db.Exec("DELETE FROM roles WHERE id = ?", id)
	})
	return id, name
}

// FR-R3: every /api/staff/ route checks a permission; a new staff route without require() fails the build.
// A Viewer gets 403 on every staff write. The staff event stream (FR-P3) lives outside /api/staff/, so it is listed.
func TestStaffRoutes_FRR3(t *testing.T) {
	var routes routeRecorder
	(&Server{}).Routes(&routes)
	var staff []string
	for _, p := range routes {
		if _, path, ok := strings.Cut(p, " "); strings.HasPrefix(path, "/api/staff/") || path == "/api/events" || !ok && strings.HasPrefix(p, "/api/staff/") {
			staff = append(staff, p)
		}
	}
	if len(staff) == 0 {
		t.Fatal("no /api/staff/ routes found")
	}

	e := newAuthEnv(t)
	_, empty := e.newRole()
	noPerms := e.session(e.newStaff(empty, true))
	viewer := e.session(e.newStaff("Viewer", true))
	wildcard := regexp.MustCompile(`\{[^}]*\}`)
	for _, p := range staff {
		method, path, ok := strings.Cut(p, " ")
		if !ok { // no method: the route takes every method, so test it as a write
			method, path = http.MethodPost, p
		}
		path = wildcard.ReplaceAllString(path, "1")
		t.Run(p, func(t *testing.T) {
			if rec := e.withCookie(method, path, nil); rec.Code != http.StatusUnauthorized {
				t.Errorf("no session = %d %s, want 401", rec.Code, rec.Body)
			}
			rec := e.withCookie(method, path, noPerms)
			var body struct{ Error string }
			if rec.Code != http.StatusForbidden || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error != "auth.forbidden" {
				t.Errorf("role without permissions = %d %s, want 403 auth.forbidden; wrap the route in require() (FR-R3)", rec.Code, rec.Body)
			}
			if method != http.MethodGet {
				if rec := e.withCookie(method, path, viewer); rec.Code != http.StatusForbidden {
					t.Errorf("Viewer = %d %s, want 403 on a write", rec.Code, rec.Body)
				}
			}
		})
	}
}
