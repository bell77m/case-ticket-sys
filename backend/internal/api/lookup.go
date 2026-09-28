package api

import (
	"cmp"
	"log/slog"
	"net/http"
	"slices"

	"ticket-app/internal/models"
)

type namedItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type floor struct {
	Name  string      `json:"name"`
	Lines []namedItem `json:"lines"` // each line's ID is the location_id sent with a new ticket
}

type building struct {
	Name   string  `json:"name"`
	Floors []floor `json:"floors"`
}

// pickName returns the name in lang, or English when that translation is missing or empty (FR-I4).
func pickName(n models.Names, lang string) string {
	if v := n[lang]; v != "" {
		return v
	}
	return n["en"]
}

// GET /api/categories?lang=th — active categories for the guest form and staff filters.
func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	var rows []models.Category
	if err := s.DB.WithContext(r.Context()).Where("is_active").Order("id").Find(&rows).Error; err != nil {
		slog.Error("list categories", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	lang := r.URL.Query().Get("lang")
	out := make([]namedItem, 0, len(rows))
	for _, c := range rows {
		out = append(out, namedItem{ID: c.ID, Name: pickName(c.Name, lang)})
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/locations?lang=th — active locations as a building → floor → line tree.
func (s *Server) locations(w http.ResponseWriter, r *http.Request) {
	var rows []models.Location
	if err := s.DB.WithContext(r.Context()).Where("is_active").Find(&rows).Error; err != nil {
		slog.Error("list locations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, buildLocationTree(rows, r.URL.Query().Get("lang")))
}

// buildLocationTree groups flat location rows. Order follows the English names,
// so the list is the same in every language.
func buildLocationTree(rows []models.Location, lang string) []building {
	slices.SortFunc(rows, func(a, b models.Location) int {
		return cmp.Or(
			cmp.Compare(a.Building["en"], b.Building["en"]),
			cmp.Compare(a.Floor["en"], b.Floor["en"]),
			cmp.Compare(a.Line["en"], b.Line["en"]),
		)
	})
	var tree []building
	var lastBuilding, lastFloor string
	for _, l := range rows {
		if len(tree) == 0 || l.Building["en"] != lastBuilding {
			tree = append(tree, building{Name: pickName(l.Building, lang), Floors: []floor{}})
			lastBuilding, lastFloor = l.Building["en"], ""
		}
		b := &tree[len(tree)-1]
		if len(b.Floors) == 0 || l.Floor["en"] != lastFloor {
			b.Floors = append(b.Floors, floor{Name: pickName(l.Floor, lang), Lines: []namedItem{}})
			lastFloor = l.Floor["en"]
		}
		f := &b.Floors[len(b.Floors)-1]
		f.Lines = append(f.Lines, namedItem{ID: l.ID, Name: pickName(l.Line, lang)})
	}
	if tree == nil {
		return []building{}
	}
	return tree
}
