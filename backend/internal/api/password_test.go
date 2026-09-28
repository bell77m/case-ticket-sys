package api

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
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

// FR-A10: a change that started before a reset landed must not undo the reset. The handler holds the staff row as
// read at the start of the request; once the password changed since, the change is refused and the reset stands.
func TestChangePassword_LosesToConcurrentReset_FRA10(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	stale := st // what requireSession read, just before Root Admin's reset committed
	resetHash, err := auth.HashPassword("temporary-pass-1")
	if err != nil {
		t.Fatal(err)
	}
	e.db.Exec("UPDATE staff SET password_hash = ?, must_change_password = true, password_changed_at = now() WHERE id = ?", resetHash, st.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(passwordBody(testPassword, "brand-new-password-1")))
	req = req.WithContext(context.WithValue(req.Context(), staffKey{}, &stale))
	rec := httptest.NewRecorder()
	(&Server{DB: e.db, Sessions: e.sessions}).changePassword(rec, req)

	if code, _ := errorBody(rec); rec.Code != http.StatusUnauthorized || code != "auth.required" || cookieNamed(rec, sessionCookie) != nil {
		t.Errorf("change racing a reset = %d %s, want 401 auth.required and no session", rec.Code, rec.Body)
	}
	if row := e.staffRow(st.ID); row.PasswordHash != resetHash || !row.MustChangePassword {
		t.Errorf("the reset was undone: must change %v, hash is the reset's %v", row.MustChangePassword, row.PasswordHash == resetHash)
	}
}
