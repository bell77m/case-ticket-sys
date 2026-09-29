package api

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"

	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

// Activity log (FR-L1, T3.06): audit_log for staff with audit.view, newest first. This is the only view that shows a
// row's target and IP address (FR-L1 lists the IP for sign-ins); the ticket timeline hides both (FR-L3).

// activityExportLimit caps the rows in one activity log PDF; tests lower it.
var activityExportLimit = 5000

// activityOrder is newest first. The created_at index serves it (Index Scan Backward plus Incremental Sort for the id
// tiebreak, checked with EXPLAIN on the dev DB), so it needs no index of its own.
const activityOrder = "created_at DESC, id DESC"

var actionCode = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)+$`)

type activityActor struct {
	Type string `json:"type"`           // guest, staff or system
	ID   *int64 `json:"id,omitempty"`   // staff only
	Name string `json:"name,omitempty"` // staff only; deactivated staff keep theirs
}

type activityItem struct {
	ID        int64         `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	Actor     activityActor `json:"actor"`
	Action    string        `json:"action"`
	TicketID  *int64        `json:"ticket_id,omitempty"`
	Target    string        `json:"target,omitempty"`
	From      string        `json:"from,omitempty"`
	To        string        `json:"to,omitempty"`
	IP        string        `json:"ip,omitempty"`
}

type activityPage struct {
	Items    []activityItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// activityFilters are the filters as sent: the query of GET /api/staff/activity, or the body of its PDF export.
// All strings, so both get the same checks and field codes; empty = not set.
type activityFilters struct {
	Staff  string   `json:"staff,omitempty"` // the actor's staff ID
	Action []string `json:"action,omitempty"`
	Ticket string   `json:"ticket,omitempty"`
	From   string   `json:"from,omitempty"` // dates in TZ, both inclusive
	To     string   `json:"to,omitempty"`
	TZ     string   `json:"tz,omitempty"`
}

// activityQuery is a checked activityFilters.
type activityQuery struct {
	staff, ticket int64
	actions       []string
	from, to, tz  string
	today         string // the date now in tz
}

// checkActivityFilters checks f; fields holds a code per bad field (never nil), err is a database failure.
func (s *Server) checkActivityFilters(ctx context.Context, f activityFilters) (q activityQuery, fields map[string]string, err error) {
	fields = map[string]string{}
	q = activityQuery{actions: f.Action, from: f.From, to: f.To, tz: cmp.Or(f.TZ, "UTC")}
	if q.today, err = s.today(ctx, q.tz); err != nil {
		return q, fields, err
	}
	if q.today == "" {
		fields["tz"] = "invalid"
	}
	id := func(name, v string) int64 {
		n, err := strconv.ParseInt(v, 10, 64)
		if v != "" && (err != nil || n < 1) {
			fields[name] = "invalid"
		}
		return n
	}
	q.staff, q.ticket = id("staff", f.Staff), id("ticket", f.Ticket)
	if len(f.Action) > 20 { // keeps one request cheap, like the queue's caps
		fields["action"] = "invalid"
	}
	for _, a := range f.Action {
		if len(a) > 64 || !actionCode.MatchString(a) {
			fields["action"] = "invalid"
		}
	}
	date := func(name, v string) (time.Time, bool) {
		if v == "" {
			return time.Time{}, false
		}
		d, ok := parseDay(v)
		if !ok {
			fields[name] = "invalid"
		}
		return d, ok
	}
	from, okFrom := date("from", f.From)
	to, okTo := date("to", f.To)
	if okFrom && okTo && to.Before(from) {
		fields["to"] = "invalid"
	}
	return q, fields, nil
}

// apply narrows db to q's rows. Day bounds are computed by PostgreSQL in q.tz (see reports.go).
func (q activityQuery) apply(db *gorm.DB) *gorm.DB {
	db = db.Model(&models.AuditEntry{})
	if q.staff != 0 {
		db = db.Where("actor_staff_id = ?", q.staff)
	}
	if len(q.actions) > 0 {
		db = db.Where("action IN ?", q.actions)
	}
	if q.ticket != 0 {
		db = db.Where("ticket_id = ?", q.ticket)
	}
	if q.from != "" {
		db = db.Where("created_at >= CAST(? AS date)::timestamp AT TIME ZONE ?", q.from, q.tz)
	}
	if q.to != "" {
		db = db.Where("created_at < (CAST(? AS date) + 1)::timestamp AT TIME ZONE ?", q.to, q.tz)
	}
	return db
}

// GET /api/staff/activity?staff=3&action=login.failed&action=login.success&ticket=12&from=2026-09-01&to=2026-09-30&tz=Asia/Bangkok&page=1&page_size=50&lang=th
// Read-only, so not audited; activity.exported belongs to the PDF export.
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	ctx, v := r.Context(), r.URL.Query()
	q, fields, err := s.checkActivityFilters(ctx, activityFilters{Staff: v.Get("staff"), Action: v["action"], Ticket: v.Get("ticket"),
		From: v.Get("from"), To: v.Get("to"), TZ: v.Get("tz")})
	if err != nil {
		internalError(w, "activity log", err)
		return
	}
	page, size := parsePage(v, 50, fields)
	if validationFailed(w, fields) {
		return
	}
	out := activityPage{Page: page, PageSize: size}
	db := s.DB.WithContext(ctx)
	var rows []models.AuditEntry
	err = q.apply(db).Count(&out.Total).Error
	if err == nil {
		err = q.apply(db).Order(activityOrder).Limit(size).Offset((page - 1) * size).Find(&rows).Error
	}
	if err == nil {
		out.Items, err = activityItems(db, rows, v.Get("lang"))
	}
	if err != nil {
		internalError(w, "activity log", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// activityItems maps rows for display, with staff names and category names in lang (FR-I6): one query each.
func activityItems(db *gorm.DB, rows []models.AuditEntry, lang string) ([]activityItem, error) {
	var staffIDs, catIDs []int64
	for _, ev := range rows {
		auditRefs(ev, &staffIDs, &catIDs)
	}
	names, catNames, err := nameMaps(db, staffIDs, catIDs, lang)
	if err != nil {
		return nil, err
	}
	items := make([]activityItem, 0, len(rows))
	for _, ev := range rows {
		it := activityItem{ID: ev.ID, CreatedAt: ev.CreatedAt, Action: ev.Action, TicketID: ev.TicketID,
			Target: optText(ev.Target), IP: optText(ev.IPAddress), Actor: activityActor{Type: ev.ActorType, ID: ev.ActorStaffID}}
		it.From, it.To = auditValues(ev, names, catNames)
		if ev.ActorStaffID != nil {
			it.Actor.Name = names[optText(ev.ActorStaffID)]
		}
		items = append(items, it)
	}
	return items, nil
}

// POST /api/staff/activity/export {"staff", "action": [...], "ticket", "from", "to", "tz", "lang"} — the filtered log
// as activity-log-YYYY-MM-DD.pdf, newest first, at most activityExportLimit rows (FR-P4, FR-I6). activity.exported
// has the filters as its target (FR-L1).
func (s *Server) exportActivity(w http.ResponseWriter, r *http.Request) {
	var in struct {
		activityFilters
		Lang string `json:"lang"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	q, fields, err := s.checkActivityFilters(r.Context(), in.activityFilters)
	if err != nil {
		internalError(w, "activity export", err)
		return
	}
	if !languages[in.Lang] {
		fields["lang"] = "invalid"
	}
	if validationFailed(w, fields) {
		return
	}
	in.TZ = q.tz
	target, _ := json.Marshal(in)
	s.exportPDF(w, r, "activity", in.activityFilters, in.Lang, "activity.exported", string(target), "activity-log-"+q.today+".pdf")
}

// activityPrintFilters are the filters as the print page shows them; unset ones are null ([] for actions).
type activityPrintFilters struct {
	Staff  *namedItem `json:"staff"`
	Action []string   `json:"action"`
	Ticket *int64     `json:"ticket"`
	From   *string    `json:"from"`
	To     *string    `json:"to"`
	TZ     string     `json:"tz"`
}

// GET /api/print/activity with X-Print-Token — the log for the print page (FR-P4): the rows, whether more than
// activityExportLimit matched (only that many are sent), the filters with the staff member's name, and lang.
func (s *Server) printActivity(w http.ResponseWriter, r *http.Request) {
	job, ok := readPrintJob[activityFilters](s, w, r, "activity", rbac.AuditView)
	if !ok {
		return
	}
	db := s.DB.WithContext(r.Context())
	q, fields, err := s.checkActivityFilters(r.Context(), job.Filters)
	if err == nil && len(fields) > 0 {
		err = fmt.Errorf("stored print filters invalid: %v", fields)
	}
	var rows []models.AuditEntry
	if err == nil {
		err = q.apply(db).Order(activityOrder).Limit(activityExportLimit + 1).Find(&rows).Error
	}
	out := struct {
		Items     []activityItem       `json:"items"`
		Truncated bool                 `json:"truncated"`
		Filters   activityPrintFilters `json:"filters"`
		Lang      string               `json:"lang"`
	}{Truncated: len(rows) > activityExportLimit, Lang: job.Lang,
		Filters: activityPrintFilters{Action: append([]string{}, q.actions...), TZ: q.tz}}
	if err == nil {
		out.Items, err = activityItems(db, rows[:min(len(rows), activityExportLimit)], job.Lang)
	}
	pf := &out.Filters
	if q.staff != 0 && err == nil {
		pf.Staff = &namedItem{ID: q.staff}
		err = db.Model(&models.Staff{}).Select("name").Where("id = ?", q.staff).Scan(&pf.Staff.Name).Error
	}
	if err != nil {
		internalError(w, "activity print", err)
		return
	}
	if q.ticket != 0 {
		pf.Ticket = &q.ticket
	}
	if q.from != "" {
		pf.From = &q.from
	}
	if q.to != "" {
		pf.To = &q.to
	}
	writeJSON(w, http.StatusOK, out)
}
