package api

import (
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
)

// Categories and locations (T2.11): staff list, add and edit them with a name in every language (FR-I4).
// Rows are deactivated, never deleted: tickets keep pointing at them.

// adminCategory and adminLocation are what staff see; the models convert to them directly.
type adminCategory struct {
	ID       int64        `json:"id"`
	Name     models.Names `json:"name"`
	IsActive bool         `json:"is_active"`
}

type adminLocation struct {
	ID       int64        `json:"id"`
	Building models.Names `json:"building"`
	Floor    models.Names `json:"floor"`
	Line     models.Names `json:"line"`
	IsActive bool         `json:"is_active"`
}

var errLookupExists = errors.New("lookup row exists")

// checkNames trims a Names object from an admin form. All four languages are required (FR-I4) under the guest
// text rules; problems go into fields as "<field>.<lang>", or "<field>" for a language the app does not have.
func checkNames(field string, in models.Names, fields map[string]string) models.Names {
	for lang := range in {
		if !languages[lang] {
			fields[field] = "invalid"
		}
	}
	out := models.Names{}
	for lang := range languages {
		out[lang] = strings.TrimSpace(in[lang])
		if p := textProblem(field, out[lang], 1, 100); p != "" {
			fields[field+"."+lang] = p
		}
	}
	return out
}

func validationFailed(w http.ResponseWriter, fields map[string]string) bool {
	if len(fields) == 0 {
		return false
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
	return true
}

// lookupTaken locks table for the rest of the transaction, then returns errLookupExists when a row other than id
// matches where. The lock serializes adds and renames, so two requests cannot both pass the check.
func lookupTaken(tx *gorm.DB, table string, id int64, where string, args ...any) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", table).Error; err != nil {
		return err
	}
	var n int64
	if err := tx.Table(table).Where("id <> ?", id).Where(where, args...).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errLookupExists
	}
	return nil
}

// categoryTaken: English names are unique, ignoring case.
func categoryTaken(tx *gorm.DB, c models.Category) error {
	return lookupTaken(tx, "categories", c.ID, "lower(name->>'en') = lower(?)", c.Name["en"])
}

// locationTaken: the English building / floor / line is unique, ignoring case. This is stricter than the
// UNIQUE constraint (whole jsonb values), which lets one place in with two sets of translations.
func locationTaken(tx *gorm.DB, l models.Location) error {
	return lookupTaken(tx, "locations", l.ID,
		"lower(building->>'en') = lower(?) AND lower(floor->>'en') = lower(?) AND lower(line->>'en') = lower(?)",
		l.Building["en"], l.Floor["en"], l.Line["en"])
}

func locationLabel(l models.Location) string {
	return l.Building["en"] + " / " + l.Floor["en"] + " / " + l.Line["en"]
}

func activeAction(kind string, active bool) string {
	if active {
		return kind + ".reactivated"
	}
	return kind + ".deactivated"
}

// GET /api/staff/categories — every category, active or not, with all names, by English name.
func (s *Server) adminCategories(w http.ResponseWriter, r *http.Request) {
	var rows []models.Category
	if err := s.DB.WithContext(r.Context()).Order("name->>'en', id").Find(&rows).Error; err != nil {
		writeLookupError(w, err, "category")
		return
	}
	out := make([]adminCategory, len(rows))
	for i, c := range rows {
		out[i] = adminCategory(c)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staff/categories {"name": {en, zh-CN, my, th}}
func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name models.Names `json:"name"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	fields := map[string]string{}
	c := models.Category{Name: checkNames("name", in.Name, fields), IsActive: true}
	if validationFailed(w, fields) {
		return
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := categoryTaken(tx, c); err != nil {
			return err
		}
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Staff(currentStaff(r).ID), "category.created", audit.Change{Target: c.Name["en"], IP: clientIP(r)})
	})
	if err != nil {
		writeLookupError(w, err, "category")
		return
	}
	writeJSON(w, http.StatusCreated, adminCategory(c))
}

// PATCH /api/staff/categories/{id} {"name"?, "is_active"?} — one audit row per changed field, none without a change.
func (s *Server) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "category.not_found")
		return
	}
	var in struct {
		Name     models.Names `json:"name"`
		IsActive *bool        `json:"is_active"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Name == nil && in.IsActive == nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	fields := map[string]string{}
	var name models.Names
	if in.Name != nil {
		name = checkNames("name", in.Name, fields)
	}
	if validationFailed(w, fields) {
		return
	}

	var c models.Category
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, id).Error; err != nil {
			return err
		}
		old := c
		if name != nil {
			c.Name = name
		}
		if in.IsActive != nil {
			c.IsActive = *in.IsActive
		}
		renamed := !maps.Equal(old.Name, c.Name)
		if !renamed && c.IsActive == old.IsActive {
			return nil
		}
		if renamed {
			if err := categoryTaken(tx, c); err != nil {
				return err
			}
		}
		if err := tx.Select("name", "is_active").Updates(&c).Error; err != nil {
			return err
		}
		actor, ip := audit.Staff(currentStaff(r).ID), clientIP(r)
		if renamed {
			change := audit.Change{Target: c.Name["en"], From: old.Name["en"], To: c.Name["en"], IP: ip}
			if err := audit.Record(tx, actor, "category.changed", change); err != nil {
				return err
			}
		}
		if c.IsActive != old.IsActive {
			return audit.Record(tx, actor, activeAction("category", c.IsActive), audit.Change{Target: c.Name["en"], IP: ip})
		}
		return nil
	})
	if err != nil {
		writeLookupError(w, err, "category")
		return
	}
	writeJSON(w, http.StatusOK, adminCategory(c))
}

// GET /api/staff/locations — every location, active or not, with all names, by English building, floor, line.
func (s *Server) adminLocations(w http.ResponseWriter, r *http.Request) {
	var rows []models.Location
	if err := s.DB.WithContext(r.Context()).Order("building->>'en', floor->>'en', line->>'en', id").Find(&rows).Error; err != nil {
		writeLookupError(w, err, "location")
		return
	}
	out := make([]adminLocation, len(rows))
	for i, l := range rows {
		out[i] = adminLocation(l)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staff/locations {"building", "floor", "line"} — to add a line under an existing floor, the client
// sends that building's and floor's Names unchanged.
func (s *Server) createLocation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Building models.Names `json:"building"`
		Floor    models.Names `json:"floor"`
		Line     models.Names `json:"line"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	fields := map[string]string{}
	l := models.Location{
		Building: checkNames("building", in.Building, fields),
		Floor:    checkNames("floor", in.Floor, fields),
		Line:     checkNames("line", in.Line, fields),
		IsActive: true,
	}
	if validationFailed(w, fields) {
		return
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := locationTaken(tx, l); err != nil {
			return err
		}
		if err := tx.Create(&l).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Staff(currentStaff(r).ID), "location.created", audit.Change{Target: locationLabel(l), IP: clientIP(r)})
	})
	if err != nil {
		writeLookupError(w, err, "location")
		return
	}
	writeJSON(w, http.StatusCreated, adminLocation(l))
}

// PATCH /api/staff/locations/{id} {"building"?, "floor"?, "line"?, "is_active"?} — as updateCategory.
func (s *Server) updateLocation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "location.not_found")
		return
	}
	var in struct {
		Building models.Names `json:"building"`
		Floor    models.Names `json:"floor"`
		Line     models.Names `json:"line"`
		IsActive *bool        `json:"is_active"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Building == nil && in.Floor == nil && in.Line == nil && in.IsActive == nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	fields := map[string]string{}
	for field, n := range map[string]*models.Names{"building": &in.Building, "floor": &in.Floor, "line": &in.Line} {
		if *n != nil {
			*n = checkNames(field, *n, fields)
		}
	}
	if validationFailed(w, fields) {
		return
	}

	var l models.Location
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&l, id).Error; err != nil {
			return err
		}
		old := l
		if in.Building != nil {
			l.Building = in.Building
		}
		if in.Floor != nil {
			l.Floor = in.Floor
		}
		if in.Line != nil {
			l.Line = in.Line
		}
		if in.IsActive != nil {
			l.IsActive = *in.IsActive
		}
		renamed := !maps.Equal(old.Building, l.Building) || !maps.Equal(old.Floor, l.Floor) || !maps.Equal(old.Line, l.Line)
		if !renamed && l.IsActive == old.IsActive {
			return nil
		}
		if renamed {
			if err := locationTaken(tx, l); err != nil {
				return err
			}
		}
		if err := tx.Select("building", "floor", "line", "is_active").Updates(&l).Error; err != nil {
			return err
		}
		actor, ip := audit.Staff(currentStaff(r).ID), clientIP(r)
		if renamed {
			change := audit.Change{Target: locationLabel(l), From: locationLabel(old), To: locationLabel(l), IP: ip}
			if err := audit.Record(tx, actor, "location.changed", change); err != nil {
				return err
			}
		}
		if l.IsActive != old.IsActive {
			return audit.Record(tx, actor, activeAction("location", l.IsActive), audit.Change{Target: locationLabel(l), IP: ip})
		}
		return nil
	})
	if err != nil {
		writeLookupError(w, err, "location")
		return
	}
	writeJSON(w, http.StatusOK, adminLocation(l))
}

// writeLookupError answers a failed category or location request; kind is "category" or "location".
func writeLookupError(w http.ResponseWriter, err error, kind string) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, kind+".not_found")
	case errors.Is(err, errLookupExists):
		writeError(w, http.StatusConflict, kind+".exists")
	default:
		slog.Error("admin "+kind, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}
