package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"ticket-app/internal/models"
)

// Reports dashboard data (FR-P1, FR-P2). All time zone math runs in PostgreSQL with AT TIME ZONE:
// the distroless image has no zoneinfo, so Go's time.LoadLocation would fail there.
//
// SQL below uses GORM named parameters. GORM ends a name only at a space, comma, ')', quote, newline or ';',
// so write CAST(@x AS type), never @x::type, and never use '?' (it would bind the parameter map).

// reportCard is one summary card: the value for the period and for the previous one.
type reportCard[T any] struct {
	Value    T `json:"value"`
	Previous T `json:"previous"`
}

// at returns the value for the current period, or for the previous one.
func (c *reportCard[T]) at(current bool) *T {
	if current {
		return &c.Value
	}
	return &c.Previous
}

type reportCards struct {
	Open                       reportCard[int64]    `json:"open"`
	Unassigned                 reportCard[int64]    `json:"unassigned"`
	UrgentOpen                 reportCard[int64]    `json:"urgent_open"`
	New                        reportCard[int64]    `json:"new"`
	Resolved                   reportCard[int64]    `json:"resolved"`
	FirstResponseMedianSeconds reportCard[*float64] `json:"first_response_median_seconds"`
	ResolutionMedianSeconds    reportCard[*float64] `json:"resolution_median_seconds"`
}

type reportDay struct {
	Date     string `json:"date"`
	Opened   int64  `json:"opened"`
	Resolved int64  `json:"resolved"`
}

type reportStatus struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type reportPriority struct {
	Priority *string `json:"priority"` // null = not triaged yet
	Count    int64   `json:"count"`
}

type reportCategory struct {
	ID    *int64  `json:"id"` // null = no category
	Name  *string `json:"name"`
	Count int64   `json:"count"`
}

type reportBuilding struct {
	Building string        `json:"building"`
	Count    int64         `json:"count"`
	Floors   []reportFloor `json:"floors"`
}

type reportFloor struct {
	Floor string       `json:"floor"`
	Count int64        `json:"count"`
	Lines []reportLine `json:"lines"`
}

type reportLine struct {
	ID    int64  `json:"id"`
	Line  string `json:"line"`
	Count int64  `json:"count"`
}

type reportWorkload struct {
	Assignee namedItem `json:"assignee"`
	Urgent   int64     `json:"urgent"`
	High     int64     `json:"high"`
	Medium   int64     `json:"medium"`
	Low      int64     `json:"low"`
	None     int64     `json:"none"`
}

func (w *reportWorkload) add(priority string, n int64) {
	switch priority {
	case "urgent":
		w.Urgent += n
	case "high":
		w.High += n
	case "medium":
		w.Medium += n
	case "low":
		w.Low += n
	default:
		w.None += n
	}
}

func (w *reportWorkload) total() int64 { return w.Urgent + w.High + w.Medium + w.Low + w.None }

type reportWeek struct {
	WeekStart            string   `json:"week_start"`
	FirstResponseSeconds *float64 `json:"first_response_seconds"`
	ResolutionSeconds    *float64 `json:"resolution_seconds"`
}

type reportPeriod struct {
	From         string `json:"from"`
	To           string `json:"to"`
	PreviousFrom string `json:"previous_from"`
	PreviousTo   string `json:"previous_to"`
}

type reportOut struct {
	Period              reportPeriod     `json:"period"`
	Cards               reportCards      `json:"cards"`
	OpenedResolvedDaily []reportDay      `json:"opened_resolved_daily"`
	OpenByStatus        []reportStatus   `json:"open_by_status"`
	OpenByPriority      []reportPriority `json:"open_by_priority"`
	ByCategory          []reportCategory `json:"by_category"`
	ByLocation          []reportBuilding `json:"by_location"`
	Workload            []reportWorkload `json:"workload"`
	WeeklyMedians       []reportWeek     `json:"weekly_medians"`
}

// reportBase is the ticket set every number starts from: the building and category filters (FR-P2).
// Category is the ticket's current one; its history is not rebuilt.
const reportBase = `WITH b AS (
    SELECT t.* FROM tickets t JOIN locations l ON l.id = t.location_id
    WHERE (@building = '' OR l.building->>'en' = @building )
      AND (CAST(@category AS bigint) = 0 OR t.category_id = @category ))`

// Flow cards for both periods: tickets created, resolved, and the median response and resolution times.
const reportFlowSQL = reportBase + `,
p (cur, s, e) AS (VALUES (true, CAST(@s AS timestamptz), CAST(@e AS timestamptz)),
                         (false, CAST(@ps AS timestamptz), CAST(@s AS timestamptz)))
SELECT p.cur,
    count(*) FILTER (WHERE b.created_at >= p.s AND b.created_at < p.e) AS created,
    count(*) FILTER (WHERE b.resolved_at >= p.s AND b.resolved_at < p.e) AS resolved,
    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM b.first_response_at - b.created_at))
        FILTER (WHERE b.created_at >= p.s AND b.created_at < p.e) AS first_response,
    percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM b.resolved_at - b.created_at))
        FILTER (WHERE b.resolved_at >= p.s AND b.resolved_at < p.e) AS resolution
FROM p LEFT JOIN b ON (b.created_at >= p.s AND b.created_at < p.e) OR (b.resolved_at >= p.s AND b.resolved_at < p.e)
GROUP BY p.cur`

// Open tickets at each period's as-of time, by status, priority and assignee. State is rebuilt from audit_log,
// so a ticket closed since then still counts as it was. The previous period's as-of is its end, which is @s.
// ponytail: three index lookups per ticket ever created (filters aside); cache or snapshot daily if it gets slow.
const reportStateSQL = reportBase + `,
p (cur, asof) AS (VALUES (true, CAST(@asof AS timestamptz)), (false, CAST(@s AS timestamptz)))
SELECT p.cur, coalesce(st.v, 'new') AS status, pr.v AS priority, sf.id AS assignee_id, sf.name AS assignee_name, count(*) AS n
FROM p JOIN b ON b.created_at <= p.asof
LEFT JOIN LATERAL (SELECT a.to_value AS v FROM audit_log a WHERE a.ticket_id = b.id
    AND a.action IN ('ticket.status_changed', 'ticket.auto_closed') AND a.created_at <= p.asof ORDER BY a.id DESC LIMIT 1) st ON true
LEFT JOIN LATERAL (SELECT a.to_value AS v FROM audit_log a WHERE a.ticket_id = b.id
    AND a.action = 'ticket.priority_changed' AND a.created_at <= p.asof ORDER BY a.id DESC LIMIT 1) pr ON true
LEFT JOIN LATERAL (SELECT a.to_value AS v FROM audit_log a WHERE a.ticket_id = b.id
    AND a.action = 'ticket.assigned' AND a.created_at <= p.asof ORDER BY a.id DESC LIMIT 1) asg ON true
LEFT JOIN staff sf ON sf.id = CAST(asg.v AS bigint)
WHERE coalesce(st.v, 'new') IN ('new', 'in_progress', 'waiting')
GROUP BY 1, 2, 3, 4, 5`

// One row per date in the period, zeros included, bucketed by date in the viewer's time zone.
const reportDailySQL = reportBase + `,
o AS (SELECT (created_at AT TIME ZONE @tz )::date AS day, count(*) AS n FROM b WHERE created_at >= @s AND created_at < @e GROUP BY 1),
r AS (SELECT (resolved_at AT TIME ZONE @tz )::date AS day, count(*) AS n FROM b WHERE resolved_at >= @s AND resolved_at < @e GROUP BY 1)
SELECT to_char(d.day, 'YYYY-MM-DD') AS date, coalesce(o.n, 0) AS opened, coalesce(r.n, 0) AS resolved
FROM (SELECT CAST(@from AS date) + i AS day FROM generate_series(0, CAST(@to AS date) - CAST(@from AS date)) i) d
LEFT JOIN o USING (day) LEFT JOIN r USING (day)
ORDER BY d.day`

// Medians per ISO week (Monday start) overlapping the period, counting only tickets inside the period.
const reportWeeklySQL = reportBase + `,
f AS (SELECT date_trunc('week', created_at AT TIME ZONE @tz ) AS wk,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM first_response_at - created_at)) AS v
      FROM b WHERE created_at >= @s AND created_at < @e GROUP BY 1),
r AS (SELECT date_trunc('week', resolved_at AT TIME ZONE @tz ) AS wk,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM resolved_at - created_at)) AS v
      FROM b WHERE resolved_at >= @s AND resolved_at < @e GROUP BY 1)
SELECT to_char(w.wk, 'YYYY-MM-DD') AS week_start, f.v AS first_response_seconds, r.v AS resolution_seconds
FROM generate_series(date_trunc('week', CAST(@from AS timestamp)), CAST(@to AS timestamp), interval '7 days') AS w (wk)
LEFT JOIN f USING (wk) LEFT JOIN r USING (wk)
ORDER BY w.wk`

const reportCategorySQL = reportBase + `
SELECT c.id, c.name, count(*) AS n FROM b LEFT JOIN categories c ON c.id = b.category_id
WHERE b.created_at >= @s AND b.created_at < @e
GROUP BY c.id ORDER BY n DESC, c.id`

// Ordered by English names, so buildings and floors come out grouped and ties keep a stable order.
const reportLocationSQL = reportBase + `
SELECT l.id, l.building, l.floor, l.line, count(*) AS n FROM b JOIN locations l ON l.id = b.location_id
WHERE b.created_at >= @s AND b.created_at < @e
GROUP BY l.id ORDER BY l.building->>'en', l.floor->>'en', n DESC, l.line->>'en'`

// reportFilters are the report filters as sent (FR-P2): the query of GET /api/staff/reports, or the body of the PDF
// export. All strings, so both get the same checks and the same field codes.
type reportFilters struct {
	From       string `json:"from"`
	To         string `json:"to"`
	TZ         string `json:"tz"`
	Building   string `json:"building"`
	CategoryID string `json:"category_id"`
}

// reportQuery is a checked reportFilters.
type reportQuery struct {
	from, to time.Time
	days     int
	tz       string
	today    string // the date now in tz
	building string
	category int64
}

// today is the date now in time zone tz, or "" when PostgreSQL does not know the zone: the filters' tz check
// (reports, activity log).
func (s *Server) today(ctx context.Context, tz string) (string, error) {
	var today string
	if err := s.DB.WithContext(ctx).Raw("SELECT to_char(now() AT TIME ZONE name, 'YYYY-MM-DD') FROM pg_timezone_names WHERE name = ?", tz).
		Scan(&today).Error; err != nil {
		return "", fmt.Errorf("check time zone: %w", err)
	}
	return today, nil
}

// parseDay parses a filter date, YYYY-MM-DD. Years before 2000 are refused, so a report's previous period never
// reaches year 0 (Go parses it, PostgreSQL rejects it).
func parseDay(v string) (time.Time, bool) {
	d, err := time.Parse(time.DateOnly, v)
	return d, err == nil && d.Year() >= 2000
}

// checkReportFilters checks f and fills in the defaults. fields holds a code per bad field (never nil);
// err is a database failure.
func (s *Server) checkReportFilters(ctx context.Context, f reportFilters) (q reportQuery, fields map[string]string, err error) {
	errs := map[string]string{}
	q.tz, q.building = cmp.Or(f.TZ, "UTC"), f.Building
	if q.today, err = s.today(ctx, q.tz); err != nil {
		return q, errs, err
	}
	if q.today == "" {
		errs["tz"] = "invalid"
	}
	date := func(name, v string, def time.Time) time.Time {
		if v == "" {
			return def
		}
		d, ok := parseDay(v)
		if !ok {
			errs[name] = "invalid"
		}
		return d
	}
	now, _ := time.Parse(time.DateOnly, q.today)
	q.to = date("to", f.To, now)
	q.from = date("from", f.From, q.to.AddDate(0, 0, -29))
	q.days = int(q.to.Sub(q.from).Hours()/24) + 1
	if errs["from"]+errs["to"]+errs["tz"] == "" && (q.days < 1 || q.days > 366) {
		errs["to"] = "invalid"
	}
	if v := f.CategoryID; v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			errs["category_id"] = "invalid"
		}
		q.category = n
	}
	return q, errs, nil
}

// GET /api/staff/reports?from=2026-09-01&to=2026-09-30&tz=Asia/Bangkok&building=HQ&category_id=2&lang=th
// Read-only, so not audited; report.exported belongs to the PDF export.
func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q, fields, err := s.checkReportFilters(r.Context(), reportFilters{From: v.Get("from"), To: v.Get("to"), TZ: v.Get("tz"),
		Building: v.Get("building"), CategoryID: v.Get("category_id")})
	if err != nil {
		s.reportFailed(w, err)
		return
	}
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
		return
	}
	out, err := s.buildReport(r.Context(), q, v.Get("lang"))
	if err != nil {
		s.reportFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// buildReport computes every card and dataset for q, with names in lang (FR-P1, FR-P2).
func (s *Server) buildReport(ctx context.Context, q reportQuery, lang string) (reportOut, error) {
	db := s.DB.WithContext(ctx)
	from, to, days, tz := q.from, q.to, q.days, q.tz
	prevFrom := from.AddDate(0, 0, -days)
	out := reportOut{
		Period: reportPeriod{From: from.Format(time.DateOnly), To: to.Format(time.DateOnly),
			PreviousFrom: prevFrom.Format(time.DateOnly), PreviousTo: from.AddDate(0, 0, -1).Format(time.DateOnly)},
		OpenByStatus: []reportStatus{}, OpenByPriority: []reportPriority{}, ByCategory: []reportCategory{},
		ByLocation: []reportBuilding{}, Workload: []reportWorkload{},
	}
	var bounds struct{ PrevStart, Start, End, AsOf time.Time }
	err := db.Raw(`SELECT CAST(@pf AS date)::timestamp AT TIME ZONE @tz AS prev_start,
		CAST(@from AS date)::timestamp AT TIME ZONE @tz AS start,
		(CAST(@to AS date) + 1)::timestamp AT TIME ZONE @tz AS "end",
		least((CAST(@to AS date) + 1)::timestamp AT TIME ZONE @tz , now()) AS as_of`,
		map[string]any{"pf": out.Period.PreviousFrom, "from": out.Period.From, "to": out.Period.To, "tz": tz}).Scan(&bounds).Error
	if err != nil {
		return out, fmt.Errorf("report bounds: %w", err)
	}
	args := map[string]any{"building": q.building, "category": q.category, "tz": tz, "from": out.Period.From, "to": out.Period.To,
		"ps": bounds.PrevStart, "s": bounds.Start, "e": bounds.End, "asof": bounds.AsOf}

	var flow []struct {
		Cur                       bool
		Created, Resolved         int64
		FirstResponse, Resolution *float64
	}
	var state []struct {
		Cur              bool
		Status, Priority string // Priority "" = none
		AssigneeID       *int64
		AssigneeName     string
		N                int64
	}
	var cats []struct {
		ID   *int64
		Name models.Names `gorm:"serializer:json"`
		N    int64
	}
	var locs []struct {
		ID                    int64
		Building, Floor, Line models.Names `gorm:"serializer:json"`
		N                     int64
	}
	err = errors.Join(
		db.Raw(reportFlowSQL, args).Scan(&flow).Error,
		db.Raw(reportStateSQL, args).Scan(&state).Error,
		db.Raw(reportDailySQL, args).Scan(&out.OpenedResolvedDaily).Error,
		db.Raw(reportWeeklySQL, args).Scan(&out.WeeklyMedians).Error,
		db.Raw(reportCategorySQL, args).Scan(&cats).Error,
		db.Raw(reportLocationSQL, args).Scan(&locs).Error,
	)
	if err != nil {
		return out, fmt.Errorf("report data: %w", err)
	}

	c := &out.Cards
	for _, f := range flow {
		*c.New.at(f.Cur), *c.Resolved.at(f.Cur) = f.Created, f.Resolved
		*c.FirstResponseMedianSeconds.at(f.Cur), *c.ResolutionMedianSeconds.at(f.Cur) = f.FirstResponse, f.Resolution
	}

	byStatus, byPriority := map[string]int64{}, map[string]int64{}
	workload := map[int64]*reportWorkload{}
	for _, st := range state {
		*c.Open.at(st.Cur) += st.N
		if st.AssigneeID == nil {
			*c.Unassigned.at(st.Cur) += st.N
		}
		if st.Priority == "urgent" {
			*c.UrgentOpen.at(st.Cur) += st.N
		}
		if !st.Cur {
			continue
		}
		byStatus[st.Status] += st.N
		byPriority[st.Priority] += st.N
		if st.AssigneeID != nil {
			wl := workload[*st.AssigneeID]
			if wl == nil {
				wl = &reportWorkload{Assignee: namedItem{ID: *st.AssigneeID, Name: st.AssigneeName}}
				workload[*st.AssigneeID] = wl
			}
			wl.add(st.Priority, st.N)
		}
	}
	for _, st := range []string{models.StatusNew, models.StatusInProgress, models.StatusWaiting} {
		out.OpenByStatus = append(out.OpenByStatus, reportStatus{st, byStatus[st]})
	}
	for _, p := range []string{"urgent", "high", "medium", "low", ""} {
		row := reportPriority{Count: byPriority[p]}
		if p != "" {
			row.Priority = &p
		}
		out.OpenByPriority = append(out.OpenByPriority, row)
	}
	for _, wl := range workload {
		out.Workload = append(out.Workload, *wl)
	}
	slices.SortFunc(out.Workload, func(a, b reportWorkload) int {
		return cmp.Or(cmp.Compare(b.total(), a.total()), cmp.Compare(a.Assignee.ID, b.Assignee.ID))
	})

	for _, ct := range cats {
		row := reportCategory{ID: ct.ID, Count: ct.N}
		if ct.ID != nil {
			name := pickName(ct.Name, lang)
			row.Name = &name
		}
		out.ByCategory = append(out.ByCategory, row)
	}

	// Rows arrive grouped by building, then floor (English names), as in buildLocationTree.
	var lastBuilding, lastFloor string
	for _, l := range locs {
		if len(out.ByLocation) == 0 || l.Building["en"] != lastBuilding {
			out.ByLocation = append(out.ByLocation, reportBuilding{Building: pickName(l.Building, lang), Floors: []reportFloor{}})
			lastBuilding, lastFloor = l.Building["en"], ""
		}
		b := &out.ByLocation[len(out.ByLocation)-1]
		if len(b.Floors) == 0 || l.Floor["en"] != lastFloor {
			b.Floors = append(b.Floors, reportFloor{Floor: pickName(l.Floor, lang), Lines: []reportLine{}})
			lastFloor = l.Floor["en"]
		}
		f := &b.Floors[len(b.Floors)-1]
		f.Lines = append(f.Lines, reportLine{ID: l.ID, Line: pickName(l.Line, lang), Count: l.N})
		f.Count += l.N
		b.Count += l.N
	}
	// Stable sorts: equal counts keep the English name order. Lines are already by count within a floor.
	slices.SortStableFunc(out.ByLocation, func(a, b reportBuilding) int { return cmp.Compare(b.Count, a.Count) })
	for _, b := range out.ByLocation {
		slices.SortStableFunc(b.Floors, func(x, y reportFloor) int { return cmp.Compare(y.Count, x.Count) })
	}
	return out, nil
}

func (s *Server) reportFailed(w http.ResponseWriter, err error) {
	slog.Error("reports", "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}
