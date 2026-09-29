package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"ticket-app/internal/models"
)

// queueItem is one ticket row as staff see it in the queue.
type queueItem struct {
	ID         int64         `json:"id"`
	Summary    string        `json:"summary"`
	Status     string        `json:"status"`
	Priority   *string       `json:"priority"`
	Category   *string       `json:"category"` // name in the requested language
	Location   trackLocation `json:"location"`
	GuestName  string        `json:"guest_name"`
	EmployeeID string        `json:"employee_id"`
	Assignee   *namedItem    `json:"assignee"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type queuePage struct {
	Items    []queueItem `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

var (
	queueStatuses   = map[string]bool{models.StatusNew: true, models.StatusInProgress: true, models.StatusWaiting: true, models.StatusResolved: true, models.StatusClosed: true}
	queuePriorities = map[string]bool{"low": true, "medium": true, "high": true, "urgent": true, "none": true}
	likeEscaper     = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
)

// queueFilter holds the parsed query. Values within one filter are ORed; different filters are ANDed.
type queueFilter struct {
	statuses, priorities []string
	assignee             string // "", "me", "none" or a staff ID
	text                 string
	page, size           int
}

func parseQueueFilter(q url.Values) (queueFilter, map[string]string) {
	f := queueFilter{statuses: q["status"], priorities: q["priority"], assignee: q.Get("assignee"),
		text: strings.TrimSpace(q.Get("q"))}
	errs := map[string]string{}
	f.page, f.size = parsePage(q, 25, errs)
	// Caps keep one request cheap: at most 10 values per filter.
	if len(f.statuses) > 10 {
		errs["status"] = "invalid"
	}
	if len(f.priorities) > 10 {
		errs["priority"] = "invalid"
	}
	for _, s := range f.statuses {
		if !queueStatuses[s] {
			errs["status"] = "invalid"
		}
	}
	for _, p := range f.priorities {
		if !queuePriorities[p] {
			errs["priority"] = "invalid"
		}
	}
	if a := f.assignee; a != "" && a != "me" && a != "none" {
		if _, err := strconv.ParseInt(a, 10, 64); err != nil {
			errs["assignee"] = "invalid"
		}
	}
	if utf8.RuneCountInString(f.text) > 200 {
		errs["q"] = "too_long"
	}
	return f, errs
}

// parsePage reads page (1–1000, default 1) and page_size (1–100, default def) from q; a bad value gets an "invalid"
// code in errs. The caps keep one request cheap: OFFSET at most 1000 pages.
func parsePage(q url.Values, def int, errs map[string]string) (page, size int) {
	page, size = 1, def
	number := func(name string, dst *int, hi int) {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > hi {
				errs[name] = "invalid"
				return
			}
			*dst = n
		}
	}
	number("page", &page, 1000)
	number("page_size", &size, 100)
	return page, size
}

func (f queueFilter) apply(db *gorm.DB, me int64) *gorm.DB {
	if len(f.statuses) > 0 {
		db = db.Where("status IN ?", f.statuses)
	}
	if len(f.priorities) > 0 {
		var set []string
		none := false
		for _, p := range f.priorities {
			if p == "none" {
				none = true
			} else {
				set = append(set, p)
			}
		}
		switch {
		case none && len(set) > 0:
			db = db.Where("(priority IN ? OR priority IS NULL)", set)
		case none:
			db = db.Where("priority IS NULL")
		default:
			db = db.Where("priority IN ?", set)
		}
	}
	switch f.assignee {
	case "":
	case "me":
		db = db.Where("assignee_id = ?", me)
	case "none":
		db = db.Where("assignee_id IS NULL")
	default:
		db = db.Where("assignee_id = ?", f.assignee)
	}
	if f.text != "" {
		// Case details use the trigram index, which works for Thai and Burmese text too.
		cond := `case_details ILIKE ? OR lower(employee_id) = lower(?)`
		args := []any{"%" + likeEscaper.Replace(f.text) + "%", f.text}
		if id, err := strconv.ParseInt(strings.TrimPrefix(f.text, "#"), 10, 64); err == nil {
			cond += " OR id = ?"
			args = append(args, id)
		}
		db = db.Where("("+cond+")", args...)
	}
	return db
}

// GET /api/staff/tickets?status=new&status=waiting&priority=high&assignee=me&q=printer&page=1&lang=th
func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, errs := parseQueueFilter(q)
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": errs})
		return
	}
	db := s.DB.WithContext(r.Context())
	me := currentStaff(r).ID
	out := queuePage{Items: []queueItem{}, Page: f.page, PageSize: f.size}

	var rows []models.Ticket
	if err := f.apply(db.Model(&models.Ticket{}), me).Count(&out.Total).Error; err != nil {
		s.queueFailed(w, err)
		return
	}
	err := f.apply(db, me).Order("created_at DESC, id DESC").Limit(f.size).Offset((f.page - 1) * f.size).Find(&rows).Error
	if err != nil {
		s.queueFailed(w, err)
		return
	}

	// Names for this page only: locations, categories, assignees.
	var locIDs, catIDs, staffIDs []int64
	for _, t := range rows {
		locIDs = append(locIDs, t.LocationID)
		if t.CategoryID != nil {
			catIDs = append(catIDs, *t.CategoryID)
		}
		if t.AssigneeID != nil {
			staffIDs = append(staffIDs, *t.AssigneeID)
		}
	}
	var locs []models.Location
	var cats []models.Category
	var staff []models.Staff
	if len(rows) > 0 {
		err = db.Where("id IN ?", locIDs).Find(&locs).Error
		if err == nil && len(catIDs) > 0 {
			err = db.Where("id IN ?", catIDs).Find(&cats).Error
		}
		if err == nil && len(staffIDs) > 0 {
			err = db.Select("id", "name").Where("id IN ?", staffIDs).Find(&staff).Error
		}
		if err != nil {
			s.queueFailed(w, err)
			return
		}
	}
	lang := q.Get("lang")
	locByID := map[int64]trackLocation{}
	for _, l := range locs {
		locByID[l.ID] = trackLocation{pickName(l.Building, lang), pickName(l.Floor, lang), pickName(l.Line, lang)}
	}
	catByID := map[int64]string{}
	for _, c := range cats {
		catByID[c.ID] = pickName(c.Name, lang)
	}
	staffByID := map[int64]string{}
	for _, st := range staff {
		staffByID[st.ID] = st.Name
	}

	for _, t := range rows {
		it := queueItem{ID: t.ID, Summary: t.Summary, Status: t.Status, Priority: t.Priority, Location: locByID[t.LocationID],
			GuestName: t.GuestName, EmployeeID: t.EmployeeID, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
		if t.CategoryID != nil {
			name := catByID[*t.CategoryID]
			it.Category = &name
		}
		if t.AssigneeID != nil {
			it.Assignee = &namedItem{ID: *t.AssigneeID, Name: staffByID[*t.AssigneeID]}
		}
		out.Items = append(out.Items, it)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) queueFailed(w http.ResponseWriter, err error) {
	slog.Error("ticket queue", "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}
