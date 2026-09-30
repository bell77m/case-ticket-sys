package api

import (
	"cmp"
	"context"
	"fmt"
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
// The report filters (FR-P1, so each dashboard card opens exactly its tickets) follow the report's rules:
// building is the English name, category the current one, dates are days in tz (reports.go).
type queueFilter struct {
	statuses, priorities []string
	assignee             string // "", "me", "none" or a staff ID
	text                 string
	page, size           int

	building    string
	category    int64
	by          string // "created" or "resolved": the column from and to apply to
	from, to    string // YYYY-MM-DD in tz
	asOf        string // YYYY-MM-DD in tz: status, priority and assignee as they were at the end of that day
	tz          string
	start, end  *time.Time // from and to as instants (queueBounds)
	asOfInstant *time.Time // the end of asOf, or now if that is later (as the report's as-of)
}

func parseQueueFilter(q url.Values) (queueFilter, map[string]string) {
	f := queueFilter{statuses: q["status"], priorities: q["priority"], assignee: q.Get("assignee"),
		text: strings.TrimSpace(q.Get("q")), building: q.Get("building"), by: cmp.Or(q.Get("by"), "created"),
		from: q.Get("from"), to: q.Get("to"), asOf: q.Get("as_of"), tz: cmp.Or(q.Get("tz"), "UTC")}
	errs := map[string]string{}
	if utf8.RuneCountInString(f.building) > 200 {
		errs["building"] = "too_long"
	}
	if v := q.Get("category_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			errs["category_id"] = "invalid"
		}
		f.category = n
	}
	if f.by != "created" && f.by != "resolved" {
		errs["by"] = "invalid"
	}
	for name, v := range map[string]string{"from": f.from, "to": f.to, "as_of": f.asOf} {
		if _, ok := parseDay(v); v != "" && !ok {
			errs[name] = "invalid"
		}
	}
	if f.from != "" && f.to != "" && errs["from"]+errs["to"] == "" && f.to < f.from {
		errs["to"] = "invalid"
	}
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

// queueBounds turns the filter's days into instants in tz, in one query that also checks tz. ok is false for a
// time zone PostgreSQL does not know.
func (s *Server) queueBounds(ctx context.Context, f *queueFilter) (ok bool, err error) {
	if f.from == "" && f.to == "" && f.asOf == "" {
		return true, nil
	}
	var b struct {
		Found            bool
		Start, End, AsOf *time.Time
	}
	err = s.DB.WithContext(ctx).Raw(`SELECT true AS found,
		CAST(NULLIF(@from, '') AS date)::timestamp AT TIME ZONE name AS start,
		(CAST(NULLIF(@to, '') AS date) + 1)::timestamp AT TIME ZONE name AS "end",
		least((CAST(NULLIF(@asof, '') AS date) + 1)::timestamp AT TIME ZONE name, now()) AS as_of
		FROM pg_timezone_names WHERE name = @tz`,
		map[string]any{"from": f.from, "to": f.to, "asof": f.asOf, "tz": f.tz}).Scan(&b).Error
	if err != nil {
		return false, fmt.Errorf("queue bounds: %w", err)
	}
	f.start, f.end, f.asOfInstant = b.Start, b.End, b.AsOf
	return b.Found, nil
}

// asOfState is the last value an audited action set on the ticket up to the as-of instant, as the report's
// state query rebuilds it (reportStateSQL). It shows no more than ticket.view_all already does: each ticket's
// timeline on the detail page comes from the same audit_log rows (FR-L3).
// ponytail: three index lookups per ticket created before as_of, run for the count and again for the page; keep
// the matching IDs once per request, or snapshot state daily, if as_of lists get slow.
const asOfState = `(SELECT a.to_value FROM audit_log a WHERE a.ticket_id = tickets.id AND a.action IN ?
	AND a.created_at <= ? ORDER BY a.id DESC LIMIT 1)`

func (f queueFilter) apply(db *gorm.DB, me int64) *gorm.DB {
	if f.building != "" {
		db = db.Where("location_id IN (SELECT id FROM locations WHERE building->>'en' = ?)", f.building)
	}
	if f.category != 0 {
		db = db.Where("category_id = ?", f.category)
	}
	col := "created_at"
	if f.by == "resolved" {
		col = "resolved_at"
	}
	if f.start != nil {
		db = db.Where(col+" >= ?", *f.start)
	}
	if f.end != nil {
		db = db.Where(col+" < ?", *f.end)
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
	if f.asOfInstant == nil {
		return f.stateFilters(db, me)
	}
	// As of: the same state filters, on the tickets that existed then, with their state rebuilt from audit_log. The
	// derived table is named tickets so stateFilters reads its columns unchanged.
	t := *f.asOfInstant
	then := db.Session(&gorm.Session{NewDB: true}).Table(`(SELECT id, coalesce(`+asOfState+`, 'new') AS status,
		`+asOfState+` AS priority, CAST(`+asOfState+` AS bigint) AS assignee_id
		FROM tickets WHERE created_at <= ?) AS tickets`,
		[]string{"ticket.status_changed", "ticket.auto_closed"}, t, []string{"ticket.priority_changed"}, t,
		[]string{"ticket.assigned"}, t, t)
	return db.Where("id IN (?)", f.stateFilters(then, me).Select("id"))
}

// stateFilters applies the status, priority and assignee filters to db's status, priority and assignee_id columns.
func (f queueFilter) stateFilters(db *gorm.DB, me int64) *gorm.DB {
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
	return db
}

// GET /api/staff/tickets?status=new&status=waiting&priority=high&assignee=me&q=printer&page=1&lang=th
func (s *Server) queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, errs := parseQueueFilter(q)
	if len(errs) == 0 {
		ok, err := s.queueBounds(r.Context(), &f)
		if err != nil {
			s.queueFailed(w, err)
			return
		}
		if !ok {
			errs["tz"] = "invalid"
		}
	}
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
