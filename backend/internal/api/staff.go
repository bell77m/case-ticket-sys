package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticket-app/internal/audit"
	"ticket-app/internal/auth"
	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

// Staff accounts (T2.08, T2.15): list, create (Root Admin only), set role, deactivate, reset password (FR-A1 to FR-A9).

type staffAccount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Role      namedItem `json:"role" gorm:"embedded;embeddedPrefix:role_"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

const accountQuery = `SELECT s.id, s.name, s.username, r.id AS role_id, r.name AS role_name, s.is_active, s.created_at
	FROM staff s JOIN roles r ON r.id = s.role_id`

// rootLock is the lock create-root-admin takes; changes that can remove a Root Admin take it too (FR-A5).
const rootLock = "SELECT pg_advisory_xact_lock(hashtext('create-root-admin'))"

var (
	errRoleNotFound  = errors.New("role not found")
	errUsernameTaken = errors.New("username taken")
	errRootAdminOnly = errors.New("only Root Admin may change the Root Admin role")
	errLastRootAdmin = errors.New("last active Root Admin")
)

// roleInfo is a role and whether it is the Root Admin role. That is the role holding role.manage: the DB
// keeps role.manage on Root Admin only (FR-A3), so the check does not depend on the role's name.
type roleInfo struct {
	ID   int64
	Name string
	Root bool
}

func findRole(db *gorm.DB, id int64) (roleInfo, error) {
	var r roleInfo
	err := db.Raw(`SELECT id, name, EXISTS (SELECT 1 FROM role_permissions p WHERE p.role_id = roles.id AND p.permission = ?) AS root
		FROM roles WHERE id = ?`, rbac.RoleManage, id).Scan(&r).Error
	if err == nil && r.ID == 0 {
		err = errRoleNotFound
	}
	return r, err
}

// GET /api/staff/accounts — every account with its role, by name.
func (s *Server) staffAccounts(w http.ResponseWriter, r *http.Request) {
	out := []staffAccount{}
	if err := s.DB.WithContext(r.Context()).Raw(accountQuery + " ORDER BY s.name, s.id").Scan(&out).Error; err != nil {
		slog.Error("list staff", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staff/accounts {"name", "username", "role_id", "password"} — Root Admin adds a staff member (FR-A1).
// The password is temporary: it must be changed at first sign-in (FR-A8).
func (s *Server) createStaff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		RoleID   int64  `json:"role_id"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	db := s.DB.WithContext(r.Context())
	name, username := strings.TrimSpace(in.Name), auth.NormalizeUsername(in.Username)
	fields := map[string]string{}
	if p := textProblem("name", name, 1, 100); p != "" {
		fields["name"] = p
	}
	if p := auth.UsernameProblem(username); p != "" {
		fields["username"] = p
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

	var out staffAccount
	err = db.Transaction(func(tx *gorm.DB) error {
		var id int64
		if err := tx.Raw(`INSERT INTO staff (name, username, role_id, password_hash, must_change_password)
			VALUES (?, ?, ?, ?, true) ON CONFLICT DO NOTHING RETURNING id`,
			name, username, role.ID, hash).Scan(&id).Error; err != nil {
			return err
		}
		if id == 0 { // the only unique column besides id
			return errUsernameTaken
		}
		if err := audit.Record(tx, audit.Staff(currentStaff(r).ID), "staff.created", audit.Change{Target: username, To: role.Name, IP: s.clientIP(r)}); err != nil {
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

// PATCH /api/staff/accounts/{id} {"role_id"?, "is_active"?} — one audit row per changed field.
func (s *Server) updateStaff(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "staff.not_found")
		return
	}
	var in struct {
		RoleID   *int64 `json:"role_id"`
		IsActive *bool  `json:"is_active"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.RoleID == nil && in.IsActive == nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}

	me, ip := currentStaff(r), s.clientIP(r)
	var out staffAccount
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// ponytail: one global lock for every staff change; they are rare. Two requests cannot both see
		// "another Root Admin stays" and remove the last two (FR-A5).
		if err := tx.Exec(rootLock).Error; err != nil {
			return err
		}
		var st models.Staff
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&st, id).Error; err != nil {
			return err
		}
		from, err := findRole(tx, st.RoleID)
		if err != nil {
			return err
		}
		to, active := from, st.IsActive
		if in.RoleID != nil {
			if to, err = findRole(tx, *in.RoleID); err != nil {
				return err
			}
		}
		if in.IsActive != nil {
			active = *in.IsActive
		}

		// FR-A4: only a Root Admin gives the Root Admin role or changes a Root Admin. The caller's role is
		// read after the lock, so a caller demoted a moment ago is refused.
		if from.Root || to.Root {
			var callerRoot bool
			if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM staff s JOIN role_permissions p ON p.role_id = s.role_id
				WHERE s.id = ? AND s.is_active AND p.permission = ?)`, me.ID, rbac.RoleManage).Scan(&callerRoot).Error; err != nil {
				return err
			}
			if !callerRoot {
				return errRootAdminOnly
			}
		}
		// FR-A5: an active Root Admin losing the role or access needs another active Root Admin left.
		if from.Root && st.IsActive && (!to.Root || !active) {
			var others int64
			if err := tx.Raw(`SELECT count(*) FROM staff s JOIN role_permissions p ON p.role_id = s.role_id
				WHERE s.id <> ? AND s.is_active AND p.permission = ?`, st.ID, rbac.RoleManage).Scan(&others).Error; err != nil {
				return err
			}
			if others == 0 {
				return errLastRootAdmin
			}
		}

		actor := audit.Staff(me.ID)
		if to.ID != from.ID {
			if err := tx.Model(&st).Update("role_id", to.ID).Error; err != nil {
				return err
			}
			if err := audit.Record(tx, actor, "staff.role_changed", audit.Change{Target: st.Username, From: from.Name, To: to.Name, IP: ip}); err != nil {
				return err
			}
		}
		if active != st.IsActive {
			action := "staff.reactivated"
			if !active {
				action = "staff.deactivated" // requireStaff re-reads the row, so open sessions end now (NFR-9)
			}
			if err := tx.Model(&st).Update("is_active", active).Error; err != nil {
				return err
			}
			if err := audit.Record(tx, actor, action, audit.Change{Target: st.Username, IP: ip}); err != nil {
				return err
			}
		}
		return tx.Raw(accountQuery+" WHERE s.id = ?", st.ID).Scan(&out).Error
	})
	if err != nil {
		writeStaffError(w, err, "update staff")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeStaffError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, "staff.not_found")
	case errors.Is(err, errRoleNotFound):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"role_id": "not_found"}})
	case errors.Is(err, errUsernameTaken):
		writeError(w, http.StatusConflict, "staff.username_taken")
	case errors.Is(err, errRootAdminOnly):
		writeError(w, http.StatusForbidden, "staff.root_admin_only")
	case errors.Is(err, errLastRootAdmin):
		writeError(w, http.StatusConflict, "staff.last_root_admin")
	default:
		slog.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

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
		return audit.Record(tx, audit.Staff(currentStaff(r).ID), "staff.password_reset", audit.Change{Target: username, IP: s.clientIP(r)})
	})
	if err != nil {
		writeStaffError(w, err, "reset password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
