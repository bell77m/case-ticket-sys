package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// reportFixture is a fixed data set in March 2020 (UTC) under a building no other test uses.
type reportFixture struct {
	building       string // English name, the building filter
	cat            int64
	loc1, loc2     int64
	staffA, staffB models.Staff
	viewer         *http.Cookie
}

func at(month time.Month, day, hour, minute int) time.Time {
	return time.Date(2020, month, day, hour, minute, 0, 0, time.UTC)
}

// reportData inserts the fixture. Tickets, locations and the category are deleted after the test;
// their audit_log rows stay (FR-L2), which is harmless because ticket IDs are never reused.
func (e *testEnv) reportData() reportFixture {
	e.t.Helper()
	n := time.Now().UnixNano()
	f := reportFixture{building: fmt.Sprintf("Report test %d", n)}
	f.staffA, f.staffB = e.newStaff("Agent", true), e.newStaff("Agent", true)
	f.viewer = e.session(e.newStaff("Viewer", true))

	// Inactive, so the public lookups other tests read never see them.
	cat := models.Category{Name: models.Names{"en": fmt.Sprintf("Report cat %d", n), "th": fmt.Sprintf("หมวดรายงาน %d", n)}}
	l1 := models.Location{Building: models.Names{"en": f.building, "th": fmt.Sprintf("ตึกรายงาน %d", n)},
		Floor: models.Names{"en": "Floor 1"}, Line: models.Names{"en": "Line A", "th": "ไลน์ A"}}
	l2 := models.Location{Building: l1.Building, Floor: models.Names{"en": "Floor 2"}, Line: models.Names{"en": "Line B"}}
	for _, v := range []any{&cat, &l1, &l2} {
		if err := e.db.Create(v).Error; err != nil {
			e.t.Fatal(err)
		}
	}
	f.cat, f.loc1, f.loc2 = cat.ID, l1.ID, l2.ID
	var ticketIDs []int64
	e.t.Cleanup(func() { // registered after newStaff, so it runs first and frees the staff rows
		e.db.Exec("DELETE FROM tickets WHERE id IN ?", ticketIDs)
		e.db.Exec("DELETE FROM locations WHERE id IN ?", []int64{l1.ID, l2.ID})
		e.db.Exec("DELETE FROM categories WHERE id = ?", cat.ID)
	})

	type event struct {
		action string
		to     any // string, or nil for "unassigned"
		at     time.Time
	}
	a, b := strconv.FormatInt(f.staffA.ID, 10), strconv.FormatInt(f.staffB.ID, 10)
	const sc, pc, as = "ticket.status_changed", "ticket.priority_changed", "ticket.assigned"
	var none *time.Time
	ptr := func(t time.Time) *time.Time { return &t }
	withCat := &f.cat
	tickets := []struct {
		loc                     int64
		cat                     *int64
		status                  string // the current row; the report rebuilds past state from audit_log
		priority                any
		created                 time.Time
		firstResponse, resolved *time.Time
		events                  []event
	}{
		// T1: responded after 1 h, resolved after 1 day, inside the period.
		{f.loc1, withCat, "resolved", "high", at(3, 2, 9, 0), ptr(at(3, 2, 10, 0)), ptr(at(3, 3, 9, 0)), []event{
			{sc, "in_progress", at(3, 2, 10, 0)}, {pc, "high", at(3, 2, 10, 0)}, {as, a, at(3, 2, 10, 0)},
			{sc, "resolved", at(3, 3, 9, 0)}}},
		// T2: urgent at the period end; lowered to medium afterwards.
		{f.loc1, withCat, "in_progress", "medium", at(3, 4, 12, 0), ptr(at(3, 4, 14, 0)), none, []event{
			{sc, "in_progress", at(3, 4, 14, 0)}, {pc, "urgent", at(3, 4, 14, 0)}, {as, a, at(3, 4, 14, 0)},
			{pc, "medium", at(3, 20, 0, 0)}}},
		// T3: new, no response; assigned, then unassigned again (to_value NULL).
		{f.loc1, nil, "new", nil, at(3, 5, 8, 0), none, none, []event{
			{as, b, at(3, 5, 9, 0)}, {as, nil, at(3, 6, 9, 0)}}},
		// T4: waiting at the period end, resolved and closed after it: the current row says closed.
		{f.loc2, withCat, "closed", "low", at(3, 6, 8, 0), ptr(at(3, 6, 8, 30)), ptr(at(3, 15, 0, 0)), []event{
			{sc, "in_progress", at(3, 6, 8, 30)}, {as, b, at(3, 6, 8, 30)}, {pc, "low", at(3, 6, 8, 30)},
			{sc, "waiting", at(3, 8, 0, 0)}, {sc, "resolved", at(3, 15, 0, 0)}, {"ticket.auto_closed", "closed", at(3, 22, 0, 0)}}},
		// T5: created 23:30 UTC on 9 March, which is 10 March in Bangkok.
		{f.loc2, nil, "in_progress", "high", at(3, 9, 23, 30), ptr(at(3, 10, 0, 0)), none, []event{
			{sc, "in_progress", at(3, 10, 0, 0)}, {pc, "high", at(3, 10, 0, 0)}, {as, a, at(3, 10, 0, 0)}}},
		// P1: previous period; resolved in it, auto-closed in the current one.
		{f.loc1, withCat, "closed", nil, at(2, 25, 10, 0), ptr(at(2, 25, 11, 0)), ptr(at(2, 26, 10, 0)), []event{
			{sc, "in_progress", at(2, 25, 11, 0)}, {sc, "resolved", at(2, 26, 10, 0)}, {"ticket.auto_closed", "closed", at(3, 4, 10, 0)}}},
		// P2: previous period, open and urgent at its end; first response and resolution in the current period.
		{f.loc2, withCat, "resolved", "urgent", at(2, 28, 10, 0), ptr(at(3, 2, 10, 0)), ptr(at(3, 9, 10, 0)), []event{
			{pc, "urgent", at(2, 28, 11, 0)}, {sc, "in_progress", at(3, 2, 10, 0)}, {as, a, at(3, 2, 10, 0)},
			{sc, "resolved", at(3, 9, 10, 0)}}},
	}
	for _, tk := range tickets {
		hash := make([]byte, 32)
		_, _ = rand.Read(hash)
		var id int64
		err := e.db.Raw(`INSERT INTO tickets (summary, case_details, category_id, priority, status, guest_name, employee_id,
			language, location_id, access_token_hash, first_response_at, resolved_at, created_at, updated_at)
			VALUES ('Report test', 'Report test ticket', ?, ?, ?, 'Guest', 'E1', 'en', ?, ?, ?, ?, ?, ?) RETURNING id`,
			tk.cat, tk.priority, tk.status, tk.loc, hash, tk.firstResponse, tk.resolved, tk.created, tk.created).Scan(&id).Error
		if err != nil || id == 0 {
			e.t.Fatalf("insert ticket: %v", err)
		}
		ticketIDs = append(ticketIDs, id)
		for _, ev := range tk.events {
			if err := e.db.Exec(`INSERT INTO audit_log (actor_type, action, ticket_id, to_value, created_at)
				VALUES ('system', ?, ?, ?, ?)`, ev.action, id, ev.to, ev.at).Error; err != nil {
				e.t.Fatal(err)
			}
		}
	}
	return f
}

// getReport calls GET /api/staff/reports and returns the top-level fields as raw JSON.
func (e *testEnv) getReport(c *http.Cookie, query string) map[string]json.RawMessage {
	e.t.Helper()
	var out map[string]json.RawMessage
	e.getJSON("/api/staff/reports?"+query, c, &out)
	return out
}

func checkReport(t *testing.T, got map[string]json.RawMessage, want []struct{ field, want string }) {
	t.Helper()
	for _, w := range want {
		if g := string(got[w.field]); g != w.want {
			t.Errorf("%s:\n got %s\nwant %s", w.field, g, w.want)
		}
	}
}

// FR-P1: every card, with its previous period, and every dataset, from fixed data. Open counts come from
// the state rebuilt out of audit_log at the period end, not from the tickets' current columns.
func TestReports_FRP1(t *testing.T) {
	e := newAuthEnv(t)
	f := e.reportData()
	got := e.getReport(f.viewer, "from=2020-03-01&to=2020-03-10&tz=UTC&lang=th&building="+url.QueryEscape(f.building))
	n := f.building[len("Report test "):]

	checkReport(t, got, []struct{ field, want string }{
		{"period", `{"from":"2020-03-01","to":"2020-03-10","previous_from":"2020-02-20","previous_to":"2020-02-29"}`},
		{"cards", `{"open":{"value":4,"previous":1},"unassigned":{"value":1,"previous":1},"urgent_open":{"value":1,"previous":1},` +
			`"new":{"value":5,"previous":2},"resolved":{"value":2,"previous":1},` +
			`"first_response_median_seconds":{"value":2700,"previous":131400},"resolution_median_seconds":{"value":475200,"previous":86400}}`},
		{"opened_resolved_daily", `[{"date":"2020-03-01","opened":0,"resolved":0},{"date":"2020-03-02","opened":1,"resolved":0},` +
			`{"date":"2020-03-03","opened":0,"resolved":1},{"date":"2020-03-04","opened":1,"resolved":0},` +
			`{"date":"2020-03-05","opened":1,"resolved":0},{"date":"2020-03-06","opened":1,"resolved":0},` +
			`{"date":"2020-03-07","opened":0,"resolved":0},{"date":"2020-03-08","opened":0,"resolved":0},` +
			`{"date":"2020-03-09","opened":1,"resolved":1},{"date":"2020-03-10","opened":0,"resolved":0}]`},
		{"open_by_status", `[{"status":"new","count":1},{"status":"in_progress","count":2},{"status":"waiting","count":1}]`},
		{"open_by_priority", `[{"priority":"urgent","count":1},{"priority":"high","count":1},{"priority":"medium","count":0},` +
			`{"priority":"low","count":1},{"priority":null,"count":1}]`},
		{"by_category", fmt.Sprintf(`[{"id":%d,"name":"หมวดรายงาน %s","count":3},{"id":null,"name":null,"count":2}]`, f.cat, n)},
		{"by_location", fmt.Sprintf(`[{"building":"ตึกรายงาน %s","count":5,"floors":[`+
			`{"floor":"Floor 1","count":3,"lines":[{"id":%d,"line":"ไลน์ A","count":3}]},`+
			`{"floor":"Floor 2","count":2,"lines":[{"id":%d,"line":"Line B","count":2}]}]}]`, n, f.loc1, f.loc2)},
		{"workload", fmt.Sprintf(`[{"assignee":{"id":%d,"name":"Test Staff"},"urgent":1,"high":1,"medium":0,"low":0,"none":0},`+
			`{"assignee":{"id":%d,"name":"Test Staff"},"urgent":0,"high":0,"medium":0,"low":1,"none":0}]`, f.staffA.ID, f.staffB.ID)},
		// 2020-03-01 is a Sunday, so its ISO week starts on 24 February; P1 and P2 fall in that week but
		// outside the period, so the week has no medians.
		{"weekly_medians", `[{"week_start":"2020-02-24","first_response_seconds":null,"resolution_seconds":null},` +
			`{"week_start":"2020-03-02","first_response_seconds":3600,"resolution_seconds":86400},` +
			`{"week_start":"2020-03-09","first_response_seconds":1800,"resolution_seconds":864000}]`},
	})
}

// FR-P2: the category, building and time zone filters apply to every number; lists stay [] when empty.
func TestReportsFilters_FRP2(t *testing.T) {
	e := newAuthEnv(t)
	f := e.reportData()
	b := "&building=" + url.QueryEscape(f.building)

	t.Run("category", func(t *testing.T) {
		got := e.getReport(f.viewer, fmt.Sprintf("from=2020-03-01&to=2020-03-10&category_id=%d%s", f.cat, b))
		checkReport(t, got, []struct{ field, want string }{
			{"cards", `{"open":{"value":2,"previous":1},"unassigned":{"value":0,"previous":1},"urgent_open":{"value":1,"previous":1},` +
				`"new":{"value":3,"previous":2},"resolved":{"value":2,"previous":1},` +
				`"first_response_median_seconds":{"value":3600,"previous":131400},"resolution_median_seconds":{"value":475200,"previous":86400}}`},
			{"by_category", fmt.Sprintf(`[{"id":%d,"name":"Report cat %s","count":3}]`, f.cat, f.building[len("Report test "):])},
			{"workload", fmt.Sprintf(`[{"assignee":{"id":%d,"name":"Test Staff"},"urgent":1,"high":0,"medium":0,"low":0,"none":0},`+
				`{"assignee":{"id":%d,"name":"Test Staff"},"urgent":0,"high":0,"medium":0,"low":1,"none":0}]`, f.staffA.ID, f.staffB.ID)},
		})
	})

	// In Bangkok (UTC+7) 10 March runs from 9 March 17:00 UTC: T5 (23:30 UTC on the 9th) opens on the 10th,
	// and P2's resolution (10:00 UTC on the 9th) falls in the previous day, which is the previous period.
	t.Run("time zone", func(t *testing.T) {
		got := e.getReport(f.viewer, "from=2020-03-10&to=2020-03-10&tz=Asia/Bangkok"+b)
		checkReport(t, got, []struct{ field, want string }{
			{"period", `{"from":"2020-03-10","to":"2020-03-10","previous_from":"2020-03-09","previous_to":"2020-03-09"}`},
			{"opened_resolved_daily", `[{"date":"2020-03-10","opened":1,"resolved":0}]`},
			{"weekly_medians", `[{"week_start":"2020-03-09","first_response_seconds":1800,"resolution_seconds":null}]`},
		})
		var cards struct{ New, Resolved struct{ Value, Previous int } }
		if err := json.Unmarshal(got["cards"], &cards); err != nil || cards.New.Value != 1 || cards.Resolved.Value != 0 || cards.Resolved.Previous != 1 {
			t.Errorf("cards = %s, want new 1, resolved 0 (previous 1)", got["cards"])
		}
	})

	t.Run("unknown building", func(t *testing.T) {
		got := e.getReport(f.viewer, "from=2020-03-01&to=2020-03-10&building=No+such+building+"+strconv.FormatInt(time.Now().UnixNano(), 10))
		checkReport(t, got, []struct{ field, want string }{
			{"cards", `{"open":{"value":0,"previous":0},"unassigned":{"value":0,"previous":0},"urgent_open":{"value":0,"previous":0},` +
				`"new":{"value":0,"previous":0},"resolved":{"value":0,"previous":0},` +
				`"first_response_median_seconds":{"value":null,"previous":null},"resolution_median_seconds":{"value":null,"previous":null}}`},
			{"by_category", `[]`}, {"by_location", `[]`}, {"workload", `[]`},
		})
	})
}

// FR-P2: bad filters get 400 validation with a code per field; the defaults are the last 30 days in UTC.
func TestReportsValidation_FRP2(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Viewer", true))
	tests := []struct{ query, want string }{
		{"from=2020-13-01", `{"from":"invalid"}`},
		{"from=2020-02-30", `{"from":"invalid"}`},
		{"from=1999-12-31&to=2000-01-01", `{"from":"invalid"}`}, // before 2000
		{"to=10-03-2020", `{"to":"invalid"}`},
		{"from=2020-03-10&to=2020-03-09", `{"to":"invalid"}`},
		{"from=2020-01-01&to=2021-01-01", `{"to":"invalid"}`}, // 367 days
		{"tz=Mars/Olympus", `{"tz":"invalid"}`},
		{"category_id=abc", `{"category_id":"invalid"}`},
		{"category_id=0", `{"category_id":"invalid"}`},
		{"from=x&tz=Nowhere&category_id=1.5", `{"category_id":"invalid","from":"invalid","tz":"invalid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := e.withCookie(http.MethodGet, "/api/staff/reports?"+tt.query, c)
			if want := `{"error":"validation","fields":` + tt.want + "}\n"; rec.Code != http.StatusBadRequest || rec.Body.String() != want {
				t.Errorf("= %d %s, want 400 %s", rec.Code, rec.Body, want)
			}
		})
	}

	t.Run("366 days is allowed", func(t *testing.T) {
		got := e.getReport(c, "from=2020-01-01&to=2020-12-31")
		checkReport(t, got, []struct{ field, want string }{
			{"period", `{"from":"2020-01-01","to":"2020-12-31","previous_from":"2018-12-31","previous_to":"2019-12-31"}`},
		})
	})

	t.Run("defaults", func(t *testing.T) {
		got := e.getReport(c, "")
		today := time.Now().UTC()
		want := fmt.Sprintf(`{"from":"%s","to":"%s","previous_from":"%s","previous_to":"%s"}`, today.AddDate(0, 0, -29).Format(time.DateOnly),
			today.Format(time.DateOnly), today.AddDate(0, 0, -59).Format(time.DateOnly), today.AddDate(0, 0, -30).Format(time.DateOnly))
		checkReport(t, got, []struct{ field, want string }{{"period", want}})
		var daily []json.RawMessage
		if json.Unmarshal(got["opened_resolved_daily"], &daily) != nil || len(daily) != 30 {
			t.Errorf("opened_resolved_daily has %d rows, want 30", len(daily))
		}
	})
}

// FR-P1, FR-R3: only roles with report.view see reports.
func TestReportsPermission_FRP1(t *testing.T) {
	e := newAuthEnv(t)
	tests := []struct {
		role string
		want int
	}{
		{"Agent", http.StatusForbidden},
		{"Viewer", http.StatusOK},
		{"Team Lead", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			if rec := e.withCookie(http.MethodGet, "/api/staff/reports", e.session(e.newStaff(tt.role, true))); rec.Code != tt.want {
				t.Errorf("= %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/reports", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session = %d, want 401", rec.Code)
	}
}
