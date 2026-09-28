package api

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

// Roles (T2.09): list, create, set permissions (FR-R2, FR-A3).

type roleOutput struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Root        bool     `json:"root"`
	Permissions []string `json:"permissions" gorm:"-"`
	StaffCount  int64    `json:"staff_count"`
	Perms       string   `json:"-"` // comma-joined, split into Permissions
}

// roleQuery takes rbac.RoleManage as its first argument: the Root Admin role is the one holding it (see roleInfo).
const roleQuery = `SELECT r.id, r.name,
	EXISTS (SELECT 1 FROM role_permissions p WHERE p.role_id = r.id AND p.permission = ?) AS root,
	coalesce((SELECT string_agg(p.permission, ',' ORDER BY p.permission) FROM role_permissions p WHERE p.role_id = r.id), '') AS perms,
	(SELECT count(*) FROM staff s WHERE s.role_id = r.id) AS staff_count
	FROM roles r`

var (
	errRoleNameTaken = errors.New("role name taken")
	errRoleRootFixed = errors.New("the Root Admin role cannot be edited")
)

func readRoles(db *gorm.DB, where string, args ...any) ([]roleOutput, error) {
	var out []roleOutput
	if err := db.Raw(roleQuery+where, append([]any{rbac.RoleManage}, args...)...).Scan(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Permissions = []string{}
		if out[i].Perms != "" {
			out[i].Permissions = strings.Split(out[i].Perms, ",") // permission codes have no commas (CHECK in 00001)
		}
	}
	return out, nil
}

// checkPermissions sorts and deduplicates a requested permission set. It returns a validation code instead
// when a value is not a permission ("invalid") or is kept for Root Admin (FR-A3, "root_only").
func checkPermissions(in []string) ([]string, string) {
	slices.Sort(in)
	in = slices.Compact(in)
	switch {
	case slices.ContainsFunc(in, func(p string) bool { return !slices.Contains(rbac.All, p) }):
		return nil, "invalid"
	case slices.Contains(in, rbac.StaffCreate) || slices.Contains(in, rbac.RoleManage):
		return nil, "root_only"
	}
	return in, ""
}

func addPermissions(tx *gorm.DB, roleID int64, perms []string) error {
	for _, p := range perms {
		if err := tx.Create(&models.RolePermission{RoleID: roleID, Permission: p}).Error; err != nil {
			return err
		}
	}
	return nil
}

// GET /api/staff/roles — every role with its permissions and number of staff, by name.
func (s *Server) roles(w http.ResponseWriter, r *http.Request) {
	out, err := readRoles(s.DB.WithContext(r.Context()), " ORDER BY r.name, r.id")
	if err != nil {
		writeRoleError(w, err, "list roles")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staff/roles {"name", "permissions"} — Root Admin adds a role (FR-R2).
func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	fields := map[string]string{}
	if p := textProblem("name", name, 1, 50); p != "" {
		fields["name"] = p
	}
	perms, p := checkPermissions(in.Permissions)
	if p != "" {
		fields["permissions"] = p
	}
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
		return
	}

	var out []roleOutput
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// Serializes role creation, so two requests cannot both pass the case-insensitive name check.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('roles'))").Error; err != nil {
			return err
		}
		var id int64
		if err := tx.Raw(`INSERT INTO roles (name) SELECT ? WHERE NOT EXISTS (SELECT 1 FROM roles WHERE lower(name) = lower(?))
			RETURNING id`, name, name).Scan(&id).Error; err != nil {
			return err
		}
		if id == 0 {
			return errRoleNameTaken
		}
		if err := addPermissions(tx, id, perms); err != nil {
			return err
		}
		if err := audit.Record(tx, audit.Staff(currentStaff(r).ID), "role.created", audit.Change{Target: name, To: strings.Join(perms, ","), IP: clientIP(r)}); err != nil {
			return err
		}
		var err error
		out, err = readRoles(tx, " WHERE r.id = ?", id)
		return err
	})
	if err != nil {
		writeRoleError(w, err, "create role")
		return
	}
	writeJSON(w, http.StatusCreated, out[0])
}

// PUT /api/staff/roles/{id}/permissions {"permissions"} — Root Admin replaces a role's permissions (FR-R2, FR-A3).
// require() reads the DB on every request, so staff of the role get the new set on their next request.
func (s *Server) setRolePermissions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "role.not_found")
		return
	}
	var in struct {
		Permissions []string `json:"permissions"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Permissions == nil { // a missing field must not wipe the role; send [] for none
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	db := s.DB.WithContext(r.Context())
	// The Root Admin role is fixed: taking role.manage off it would lock everyone out of role management.
	// A role's root status cannot change later: the DB refuses role.manage on any other role (00001).
	role, err := findRole(db, id)
	if err == nil && role.Root {
		err = errRoleRootFixed
	}
	if err != nil {
		writeRoleError(w, err, "read role")
		return
	}
	perms, p := checkPermissions(in.Permissions)
	if p != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"permissions": p}})
		return
	}

	var out []roleOutput
	err = db.Transaction(func(tx *gorm.DB) error {
		var locked int64 // row lock: two edits of one role cannot interleave their diffs
		if err := tx.Raw("SELECT id FROM roles WHERE id = ? FOR UPDATE", id).Scan(&locked).Error; err != nil {
			return err
		}
		if locked == 0 {
			return errRoleNotFound
		}
		var current []string
		if err := tx.Model(&models.RolePermission{}).Where("role_id = ?", id).Order("permission").Pluck("permission", &current).Error; err != nil {
			return err
		}
		// Both stay sorted, as perms and current are. removed reuses current, so it goes second.
		added := slices.DeleteFunc(slices.Clone(perms), func(p string) bool { return slices.Contains(current, p) })
		removed := slices.DeleteFunc(current, func(p string) bool { return slices.Contains(perms, p) })
		if len(added) > 0 || len(removed) > 0 {
			if len(removed) > 0 {
				if err := tx.Exec("DELETE FROM role_permissions WHERE role_id = ? AND permission IN ?", id, removed).Error; err != nil {
					return err
				}
			}
			if err := addPermissions(tx, id, added); err != nil {
				return err
			}
			change := audit.Change{Target: role.Name, From: strings.Join(removed, ","), To: strings.Join(added, ","), IP: clientIP(r)}
			if err := audit.Record(tx, audit.Staff(currentStaff(r).ID), "role.changed", change); err != nil {
				return err
			}
		}
		var err error
		out, err = readRoles(tx, " WHERE r.id = ?", id)
		return err
	})
	if err != nil {
		writeRoleError(w, err, "set role permissions")
		return
	}
	writeJSON(w, http.StatusOK, out[0])
}

func writeRoleError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, errRoleNotFound):
		writeError(w, http.StatusNotFound, "role.not_found")
	case errors.Is(err, errRoleNameTaken):
		writeError(w, http.StatusConflict, "role.name_taken")
	case errors.Is(err, errRoleRootFixed):
		writeError(w, http.StatusForbidden, "role.root_fixed")
	default:
		slog.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}
