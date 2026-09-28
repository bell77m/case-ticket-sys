package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

// randomIP is a client address of its own, so the per-IP login limit (FR-A11) never carries over between tests or runs.
func randomIP() string {
	return fmt.Sprintf("10.%d.%d.%d", rand.IntN(256), rand.IntN(256), rand.IntN(256))
}

// newAuthEnv is a testEnv with its own client IP.
func newAuthEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	e.ip = randomIP()
	return e
}

// testPassword is every test staff member's password; testHash is its hash, computed once (hashing is slow on purpose).
const testPassword = "test-password-123"

var testHash = sync.OnceValue(func() string {
	h, err := auth.HashPassword(testPassword)
	if err != nil {
		panic(err)
	}
	return h
})

// newStaff adds a staff member with a unique username and password testPassword. Cleanup deletes it,
// or only deactivates it once audit_log points at it (FR-L2 forbids deleting those audit rows).
func (e *testEnv) newStaff(role string, active bool) models.Staff {
	e.t.Helper()
	n := time.Now().UnixNano()
	st := models.Staff{
		Name: "Test Staff", Username: fmt.Sprintf("t%d", n), PasswordHash: testHash(),
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

// dropStaff deletes a test staff member, or only deactivates it once audit_log points at it (FR-L2).
func (e *testEnv) dropStaff(id int64) {
	if e.db.Exec("DELETE FROM staff WHERE id = ?", id).Error != nil {
		e.db.Exec("UPDATE staff SET is_active = false WHERE id = ?", id)
	}
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

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.MaxAge >= 0 && c.Value != "" {
			return c
		}
	}
	return nil
}

// withCookie sends a request carrying the session cookie.
func (e *testEnv) withCookie(method, path string, c *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// auditRow returns the actor type of the latest audit row for action and target, or "" if none.
func (e *testEnv) auditRow(action, target string) (actorType string, staffID *int64, ip *string) {
	e.t.Helper()
	var row models.AuditEntry
	err := e.db.Where("action = ? AND target = ?", action, target).Order("id DESC").Limit(1).Find(&row).Error
	if err != nil {
		e.t.Fatal(err)
	}
	return row.ActorType, row.ActorStaffID, row.IPAddress
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
		if _, err := e.sessions.LoginAttempt(context.Background(), fmt.Sprintf("guess%d-%d", i, time.Now().UnixNano()), e.ip); err != nil {
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

// FR-A11: parallel attempts cannot slip past the limit together. Of 10 wrong passwords sent at once for one
// username, only 5 get a password check; the rest get 429.
func TestLogin_ParallelLimit_FRA11(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	codes := make(chan int, 10)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { codes <- e.login(st.Username, "wrong-password-1").Code })
	}
	wg.Wait()
	close(codes)
	got := map[int]int{}
	for c := range codes {
		got[c]++
	}
	if got[http.StatusUnauthorized] != 5 || got[http.StatusTooManyRequests] != 5 {
		t.Errorf("10 parallel wrong passwords = %v, want 5 × 401 and 5 × 429", got)
	}
}

// FR-A11 (T2.14 review): a successful sign-in takes back only its own attempt. Failures against other usernames from
// the same IP stay counted, so staff signing in from a shared office IP never reset an attacker's count.
func TestLogin_SuccessDoesNotRefundIP_FRA11(t *testing.T) {
	e := newAuthEnv(t)
	st := e.newStaff("Agent", true)
	for i := range 19 { // 19 failed guesses against other usernames, counted straight into Redis
		if _, err := e.sessions.LoginAttempt(context.Background(), fmt.Sprintf("guess%d-%d", i, time.Now().UnixNano()), e.ip); err != nil {
			t.Fatal(err)
		}
	}
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusOK {
		t.Fatalf("staff sign-in = %d, want 200", rec.Code)
	}
	if rec := e.login(fmt.Sprintf("guess-%d", time.Now().UnixNano()), "wrong-password-1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("20th failure = %d, want 401", rec.Code)
	}
	if rec := e.login(st.Username, testPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after 20 failures from the IP, sign-in = %d, want 429 (the success must not have refunded a failure)", rec.Code)
	}
}
