package api

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

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
	ctx, ip := r.Context(), s.clientIP(r)
	username := auth.NormalizeUsername(in.Username)

	blocked, err := s.Sessions.LoginAttempt(ctx, username, ip)
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
	if !auth.CheckPassword(st.PasswordHash, in.Password) { // the attempt LoginAttempt counted stays counted
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
	if err := s.Sessions.LoginSucceeded(ctx, username, ip); err != nil {
		slog.Error("sign in", "error", err)
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

// require lets a request through only if the signed-in staff member's role has permission (FR-R3).
// ponytail: one indexed DB query per request, no cache; the check is always fresh, so role changes apply at once. Cache per role in Redis (invalidated by the roles API) if this shows up in profiles.
func (s *Server) require(permission string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireStaff(func(w http.ResponseWriter, r *http.Request) {
		var n int64
		err := s.DB.WithContext(r.Context()).Model(&models.RolePermission{}).
			Where("role_id = ? AND permission = ?", currentStaff(r).RoleID, permission).Count(&n).Error
		if err != nil {
			slog.Error("read permission", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if n == 0 {
			writeError(w, http.StatusForbidden, "auth.forbidden")
			return
		}
		next(w, r)
	})
}

// stillAllowed runs require(permission)'s checks again for a request that is still open, such as an event stream
// that outlives the request that authorized it (NFR-9, FR-R3).
func (s *Server) stillAllowed(r *http.Request, permission string) bool {
	ok := false
	s.require(permission, func(http.ResponseWriter, *http.Request) { ok = true })(discardWriter{}, r)
	return ok
}

// discardWriter drops a response; stillAllowed only needs to know whether the checks passed.
type discardWriter struct{}

func (discardWriter) Header() http.Header         { return http.Header{} }
func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardWriter) WriteHeader(int)             {}

// currentStaff is the signed-in staff member; only valid inside requireStaff.
func currentStaff(r *http.Request) *models.Staff {
	return r.Context().Value(staffKey{}).(*models.Staff)
}

type meOutput struct {
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	Username           string   `json:"username"`
	Role               string   `json:"role"`
	Language           string   `json:"language"`
	Permissions        []string `json:"permissions"`
	MustChangePassword bool     `json:"must_change_password"` // the frontend sends staff to /staff/password (FR-A8)
}

// me tells the frontend who is signed in. Permissions are for showing controls only (FR-R3).
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	st := currentStaff(r)
	db := s.DB.WithContext(r.Context())
	out := meOutput{ID: st.ID, Name: st.Name, Username: st.Username, Language: st.Language,
		MustChangePassword: st.MustChangePassword, Permissions: []string{}}
	if err := db.Raw("SELECT name FROM roles WHERE id = ?", st.RoleID).Scan(&out.Role).Error; err != nil {
		slog.Error("read role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := db.Model(&models.RolePermission{}).Where("role_id = ?", st.RoleID).Order("permission").Pluck("permission", &out.Permissions).Error; err != nil {
		slog.Error("read permissions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

var errPasswordChanged = errors.New("password changed since the session was checked")

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
	ctx, me, ip := r.Context(), currentStaff(r), s.clientIP(r)
	blocked, err := s.Sessions.LoginAttempt(ctx, me.Username, ip)
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
	if !auth.CheckPassword(me.PasswordHash, in.Current) { // the attempt LoginAttempt counted stays counted
		fields["current_password"] = "wrong"
	} else if err := s.Sessions.LoginSucceeded(ctx, me.Username, ip); err != nil {
		slog.Error("change password", "error", err)
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
		// Only if the password is still the one this request checked: a reset that landed meanwhile wins (FR-A10).
		if err := tx.Raw(`UPDATE staff SET password_hash = ?, must_change_password = false, password_changed_at = now()
			WHERE id = ? AND password_changed_at = ? RETURNING password_changed_at`, hash, me.ID, me.PasswordChangedAt).Scan(&changed).Error; err != nil {
			return err
		}
		if changed.IsZero() {
			return errPasswordChanged
		}
		return audit.Record(tx, audit.Staff(me.ID), "staff.password_changed", audit.Change{Target: me.Username, IP: ip})
	})
	if errors.Is(err, errPasswordChanged) { // this session ended with that reset
		writeError(w, http.StatusUnauthorized, "auth.required")
		return
	}
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
