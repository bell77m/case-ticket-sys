package api

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// activityFixture is a set of audit rows in June 2003 (UTC) inside the test's rolled-back transaction: a ticket ID
// no real ticket has, two staff members of their own (B deactivated) and a category with a Thai name.
type activityFixture struct {
	ticket int64
	a, b   models.Staff
	lead   *http.Cookie // a Team Lead: holds audit.view
	leadID int64
	row    map[string]int64 // fixture row ID by name
}

func (e *testEnv) activityData() activityFixture {
	e.t.Helper()
	e.isolated() // audit_log is append-only (FR-L2): the rollback is what removes these rows
	f := activityFixture{ticket: time.Now().UnixNano(), row: map[string]int64{}}
	f.a, f.b = e.newStaff("Agent", true), e.newStaff("Agent", true)
	lead := e.newStaff("Team Lead", true)
	f.lead, f.leadID = e.session(lead), lead.ID
	cat := models.Category{Name: models.Names{"en": "Activity cat", "th": "หมวดกิจกรรม"}}
	must := func(err error) {
		if err != nil {
			e.t.Fatal(err)
		}
	}
	must(e.db.Create(&cat).Error)
	must(e.db.Exec("UPDATE staff SET name = 'Activity A' WHERE id = ?", f.a.ID).Error)
	must(e.db.Exec("UPDATE staff SET name = 'Activity B', is_active = false WHERE id = ?", f.b.ID).Error)

	s := func(v string) *string { return &v }
	at := func(day, hour, minute int) time.Time {
		return time.Date(2003, time.June, day, hour, minute, 0, 0, time.UTC)
	}
	tk, a, b := &f.ticket, &f.a.ID, &f.b.ID
	for _, r := range []struct {
		name string
		row  models.AuditEntry
	}{
		{"created", models.AuditEntry{ActorType: models.ActorGuest, Action: "ticket.created", TicketID: tk, Target: s("E1234"),
			ToValue: s("location:1"), IPAddress: s("10.0.0.1"), CreatedAt: at(10, 9, 0)}},
		{"status", models.AuditEntry{ActorType: models.ActorStaff, ActorStaffID: a, Action: "ticket.status_changed", TicketID: tk,
			FromValue: s("new"), ToValue: s("in_progress"), IPAddress: s("10.0.0.2"), CreatedAt: at(10, 10, 0)}},
		// 11 June in Bangkok (UTC+7).
		{"assigned", models.AuditEntry{ActorType: models.ActorStaff, ActorStaffID: a, Action: "ticket.assigned", TicketID: tk,
			ToValue: s(itoa(f.b.ID)), CreatedAt: at(10, 23, 30)}},
		{"category", models.AuditEntry{ActorType: models.ActorStaff, ActorStaffID: a, Action: "ticket.category_changed", TicketID: tk,
			ToValue: s(itoa(cat.ID)), CreatedAt: at(11, 8, 0)}},
		// Same time as "category": the higher ID comes first.
		{"login", models.AuditEntry{ActorType: models.ActorStaff, ActorStaffID: b, Action: "login.success", Target: s(f.b.Username),
			IPAddress: s("10.9.8.7"), CreatedAt: at(11, 8, 0)}},
		{"closed", models.AuditEntry{ActorType: models.ActorSystem, Action: "ticket.auto_closed", TicketID: tk,
			FromValue: s("resolved"), ToValue: s("closed"), CreatedAt: at(12, 0, 0)}},
	} {
		must(e.db.Create(&r.row).Error)
		f.row[r.name] = r.row.ID
	}
	return f
}

// names maps item IDs back to fixture row names, for readable failures.
func (f activityFixture) names(items []activityItem) []string {
	out := []string{}
	for _, it := range items {
		name := fmt.Sprintf("other row %d", it.ID)
		for n, id := range f.row {
			if id == it.ID {
				name = n
			}
		}
		out = append(out, name)
	}
	return out
}

// activityLine is an item as one line: action, actor type/id/name, ticket, target, from>to and ip.
func activityLine(it activityItem) string {
	return fmt.Sprintf("%s %s/%s/%s ticket=%s target=%s %s>%s ip=%s", it.Action, it.Actor.Type, optText(it.Actor.ID),
		it.Actor.Name, optText(it.TicketID), it.Target, it.From, it.To, it.IP)
}

func (e *testEnv) getActivity(c *http.Cookie, query string) activityPage {
	e.t.Helper()
	var out activityPage
	e.getJSON("/api/staff/activity?"+query, c, &out)
	return out
}

// FR-L1, FR-I6: a Team Lead sees each row with the actor's name (deactivated staff too), the ticket, the target, the
// old and new values (assignee and category IDs as names, categories in the viewer's language) and the IP address.
func TestActivity_FRL1(t *testing.T) {
	e := newAuthEnv(t)
	f := e.activityData()
	tk, a, b := itoa(f.ticket), itoa(f.a.ID), itoa(f.b.ID)
	tests := []struct {
		name, query string
		want        []string
	}{
		{"ticket rows, newest first", "ticket=" + tk + "&lang=th", []string{
			"ticket.auto_closed system// ticket=" + tk + " target= resolved>closed ip=",
			"ticket.category_changed staff/" + a + "/Activity A ticket=" + tk + " target= >หมวดกิจกรรม ip=",
			"ticket.assigned staff/" + a + "/Activity A ticket=" + tk + " target= >Activity B ip=",
			"ticket.status_changed staff/" + a + "/Activity A ticket=" + tk + " target= new>in_progress ip=10.0.0.2",
			"ticket.created guest// ticket=" + tk + " target=E1234 >location:1 ip=10.0.0.1",
		}},
		{"deactivated staff, English fallback", "staff=" + b + "&lang=my", []string{
			"login.success staff/" + b + "/Activity B ticket= target=" + f.b.Username + " > ip=10.9.8.7",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			got := e.getActivity(f.lead, tt.query)
			var lines []string
			for _, it := range got.Items {
				lines = append(lines, activityLine(it))
			}
			if !slices.Equal(lines, tt.want) {
				t.Errorf("items:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}

	t.Run("created_at and defaults", func(t *testing.T) {
		e.t = t
		got := e.getActivity(f.lead, "staff="+b)
		if len(got.Items) != 1 || !got.Items[0].CreatedAt.Equal(time.Date(2003, time.June, 11, 8, 0, 0, 0, time.UTC)) {
			t.Errorf("items = %+v, want the login at 2003-06-11 08:00 UTC", got.Items)
		}
		if got.Total != 1 || got.Page != 1 || got.PageSize != 50 {
			t.Errorf("total, page, page_size = %d, %d, %d; want 1, 1, 50", got.Total, got.Page, got.PageSize)
		}
	})
}

// FR-L1: each filter narrows the log (values of one filter are ORed, filters are ANDed); dates are whole days in tz;
// pages hold page_size rows, newest first, with the total of all pages.
func TestActivityFilters_FRL1(t *testing.T) {
	e := newAuthEnv(t)
	f := e.activityData()
	tk := "ticket=" + itoa(f.ticket)
	all := []string{"closed", "category", "assigned", "status", "created"}
	tests := []struct {
		name, query string
		want        []string
		total       int64
	}{
		{"ticket", tk, all, 5},
		{"staff", "staff=" + itoa(f.a.ID), []string{"category", "assigned", "status"}, 3},
		{"actions", tk + "&action=ticket.created&action=ticket.auto_closed", []string{"closed", "created"}, 2},
		{"no match", tk + "&action=login.failed", []string{}, 0},
		{"staff and action", "staff=" + itoa(f.a.ID) + "&action=ticket.assigned", []string{"assigned"}, 1},
		{"from, UTC", tk + "&from=2003-06-11", []string{"closed", "category"}, 2},
		{"from, Bangkok", tk + "&from=2003-06-11&tz=Asia/Bangkok", []string{"closed", "category", "assigned"}, 3},
		{"to, UTC", tk + "&to=2003-06-10", []string{"assigned", "status", "created"}, 3},
		{"to, Bangkok", tk + "&to=2003-06-10&tz=Asia/Bangkok", []string{"status", "created"}, 2},
		{"one day", "staff=" + itoa(f.a.ID) + "&from=2003-06-11&to=2003-06-11", []string{"category"}, 1},
		// 2003 holds only this fixture's rows; equal times come newest ID first.
		{"same time, by id", "from=2003-06-11&to=2003-06-11", []string{"login", "category"}, 2},
		{"page 1", tk + "&page_size=2", []string{"closed", "category"}, 5},
		{"page 2", tk + "&page_size=2&page=2", []string{"assigned", "status"}, 5},
		{"last page", tk + "&page_size=2&page=3", []string{"created"}, 5},
		{"past the end", tk + "&page_size=2&page=4", []string{}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			got := e.getActivity(f.lead, tt.query)
			if names := f.names(got.Items); !slices.Equal(names, tt.want) || got.Total != tt.total {
				t.Errorf("items %v total %d, want %v total %d", names, got.Total, tt.want, tt.total)
			}
		})
	}
}

// FR-R3 (T3.06 done criterion): the activity log needs audit.view. Root Admin, Admin and Team Lead see it; an Agent
// and a Viewer get 403; no session gets 401.
func TestActivityPermission_FRR3(t *testing.T) {
	e := newAuthEnv(t)
	tests := []struct {
		role string
		want int
	}{
		{"Root Admin", http.StatusOK},
		{"Admin", http.StatusOK},
		{"Team Lead", http.StatusOK},
		{"Agent", http.StatusForbidden},
		{"Viewer", http.StatusForbidden},
		{"", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(cmp.Or(tt.role, "no session"), func(t *testing.T) {
			e.t = t
			var c *http.Cookie
			if tt.role != "" {
				c = e.session(e.newStaff(tt.role, true))
			}
			if rec := e.withCookie(http.MethodGet, "/api/staff/activity?page_size=1", c); rec.Code != tt.want {
				t.Errorf("= %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

// FR-L1: bad filters get 400 validation with a code per field, as the queue and reports do.
func TestActivityValidation_FRL1(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Team Lead", true))
	tests := []struct{ query, want string }{
		{"staff=abc", `{"staff":"invalid"}`},
		{"staff=0", `{"staff":"invalid"}`},
		{"ticket=-1", `{"ticket":"invalid"}`},
		{"ticket=1.5", `{"ticket":"invalid"}`},
		{"action=", `{"action":"invalid"}`},
		{"action=Login.Failed", `{"action":"invalid"}`},
		{"action=login", `{"action":"invalid"}`},
		{"action=login." + strings.Repeat("a", 60), `{"action":"invalid"}`},
		{strings.Repeat("action=login.failed&", 21), `{"action":"invalid"}`},
		{"from=2003-02-30", `{"from":"invalid"}`},
		{"from=1999-12-31", `{"from":"invalid"}`}, // before 2000, as in reports
		{"to=11-06-2003", `{"to":"invalid"}`},
		{"from=2003-06-12&to=2003-06-11", `{"to":"invalid"}`},
		{"tz=Mars/Olympus", `{"tz":"invalid"}`},
		{"page=0", `{"page":"invalid"}`},
		{"page=1001", `{"page":"invalid"}`},
		{"page_size=101", `{"page_size":"invalid"}`},
		{"page_size=x", `{"page_size":"invalid"}`},
		{"staff=x&ticket=y&tz=Nowhere&page=0", `{"page":"invalid","staff":"invalid","ticket":"invalid","tz":"invalid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := e.withCookie(http.MethodGet, "/api/staff/activity?"+tt.query, c)
			if want := `{"error":"validation","fields":` + tt.want + "}\n"; rec.Code != http.StatusBadRequest || rec.Body.String() != want {
				t.Errorf("= %d %s, want 400 %s", rec.Code, rec.Body, want)
			}
		})
	}
	t.Run("20 actions, a page at the caps", func(t *testing.T) {
		e.t = t
		e.getActivity(c, strings.Repeat("action=login.failed&", 20)+"page=1000&page_size=100")
	})
}

// printActivityOut is GET /api/print/activity's answer.
type printActivityOut struct {
	Items     []activityItem
	Truncated bool
	Filters   json.RawMessage
	Lang      string
}

// FR-P4, FR-L1, FR-I6: a Team Lead exports the filtered log as activity-log-YYYY-MM-DD.pdf. Gotenberg gets the
// /print/activity page with a one-time token; activity.exported is audited with the filters; the token opens
// /api/print/activity once and never /api/print/report, and a report token never opens the activity data.
func TestActivityExport_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	f := e.activityData()
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	before := e.lastAuditID()
	body := mustJSON(map[string]any{"staff": itoa(f.a.ID), "action": []string{"ticket.assigned", "ticket.category_changed", "ticket.created"},
		"ticket": itoa(f.ticket), "from": "2003-06-10", "to": "2003-06-12", "tz": "Asia/Bangkok", "lang": "th"})

	rec := e.sendJSON(http.MethodPost, "/api/staff/activity/export", f.lead, body)
	today := time.Now().UTC().Add(7 * time.Hour).Format(time.DateOnly) // Bangkok is UTC+7 all year
	for _, h := range []struct{ name, want string }{
		{"Content-Type", "application/pdf"},
		{"Content-Disposition", `attachment; filename="activity-log-` + today + `.pdf"`},
		{"Cache-Control", "no-store"},
	} {
		if got := rec.Header().Get(h.name); got != h.want {
			t.Errorf("%s = %q, want %q", h.name, got, h.want)
		}
	}
	if rec.Code != http.StatusOK || rec.Body.String() != fakePDF {
		t.Fatalf("export = %d %q, want 200 %q", rec.Code, rec.Body, fakePDF)
	}

	t.Run("audit FR-L1", func(t *testing.T) {
		e.t = t
		rows := e.exportAudits("activity.exported", before)
		want := `{"staff":"` + itoa(f.a.ID) + `","action":["ticket.assigned","ticket.category_changed","ticket.created"],"ticket":"` +
			itoa(f.ticket) + `","from":"2003-06-10","to":"2003-06-12","tz":"Asia/Bangkok","lang":"th"}`
		if len(rows) != 1 || rows[0].ActorStaffID == nil || *rows[0].ActorStaffID != f.leadID || optText(rows[0].Target) != want {
			t.Errorf("activity.exported rows = %+v, want one by staff %d with target %s", rows, f.leadID, want)
		}
	})

	token := g.token(t, "activity")
	t.Run("not the report", func(t *testing.T) {
		if rec := e.printData("report", token); rec.Code != http.StatusNotFound {
			t.Errorf("GET /api/print/report = %d %s, want 404", rec.Code, rec.Body)
		}
	})

	t.Run("print page data, once", func(t *testing.T) {
		rec := e.printData("activity", token)
		var got printActivityOut
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("print = %d %s", rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		// Bangkok days: "assigned" (23:30 UTC on 10 June) is 11 June; "status" is excluded by the actions, "created" by the staff.
		if names := f.names(got.Items); !slices.Equal(names, []string{"category", "assigned"}) || got.Truncated || got.Lang != "th" {
			t.Errorf("items %v truncated %v lang %q, want [category assigned] false th", names, got.Truncated, got.Lang)
		}
		if len(got.Items) > 0 && got.Items[0].To != "หมวดกิจกรรม" {
			t.Errorf("category to = %q, want the Thai name (FR-I6)", got.Items[0].To)
		}
		want := `{"staff":{"id":` + itoa(f.a.ID) + `,"name":"Activity A"},"action":["ticket.assigned","ticket.category_changed","ticket.created"],` +
			`"ticket":` + itoa(f.ticket) + `,"from":"2003-06-10","to":"2003-06-12","tz":"Asia/Bangkok"}`
		if string(got.Filters) != want {
			t.Errorf("filters =\n%s\nwant\n%s", got.Filters, want)
		}
		if rec := e.printData("activity", token); rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"print.not_found"}`+"\n" {
			t.Errorf("second use = %d %s, want 404 print.not_found", rec.Code, rec.Body)
		}
	})

	t.Run("a report token", func(t *testing.T) {
		e.t = t
		tok := e.exportToken(g, "report", f.lead, `{"from": "2020-03-01", "to": "2020-03-10", "lang": "en"}`)
		if rec := e.printData("activity", tok); rec.Code != http.StatusNotFound {
			t.Errorf("GET /api/print/activity = %d %s, want 404", rec.Code, rec.Body)
		}
		if rec := e.printData("report", tok); rec.Code != http.StatusOK {
			t.Errorf("GET /api/print/report = %d %s, want 200", rec.Code, rec.Body)
		}
	})
}

// FR-P4: the PDF holds at most activityExportLimit rows, newest first, and the print data says when rows were cut.
// Unset filters are null ([] for actions).
func TestActivityExportCap_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	f := e.activityData()
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	t.Cleanup(func(n int) func() { return func() { activityExportLimit = n } }(activityExportLimit))
	tests := []struct {
		limit     int
		truncated bool
		want      []string
	}{
		{2, true, []string{"closed", "category"}},
		{5, false, []string{"closed", "category", "assigned", "status", "created"}}, // exactly the limit: nothing cut
		{6, false, []string{"closed", "category", "assigned", "status", "created"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.limit), func(t *testing.T) {
			e.t = t
			activityExportLimit = tt.limit
			rec := e.printData("activity", e.exportToken(g, "activity", f.lead, mustJSON(map[string]string{"ticket": itoa(f.ticket), "lang": "en"})))
			var got printActivityOut
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
				t.Fatalf("print = %d %s", rec.Code, rec.Body)
			}
			if names := f.names(got.Items); !slices.Equal(names, tt.want) || got.Truncated != tt.truncated {
				t.Errorf("items %v truncated %v, want %v %v", names, got.Truncated, tt.want, tt.truncated)
			}
			want := `{"staff":null,"action":[],"ticket":` + itoa(f.ticket) + `,"from":null,"to":null,"tz":"UTC"}`
			if string(got.Filters) != want {
				t.Errorf("filters = %s, want %s", got.Filters, want)
			}
		})
	}
}

// FR-P4, FR-R3: exporting the log needs audit.view, like reading it; a Viewer (report.view only) gets 403.
func TestActivityExportPermission_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	tests := []struct {
		role string
		want int
	}{
		{"Team Lead", http.StatusOK},
		{"Agent", http.StatusForbidden},
		{"Viewer", http.StatusForbidden},
		{"", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(cmp.Or(tt.role, "no session"), func(t *testing.T) {
			e.t = t
			var c *http.Cookie
			if tt.role != "" {
				c = e.session(e.newStaff(tt.role, true))
			}
			body := `{"from": "2003-06-10", "to": "2003-06-10", "lang": "en"}`
			if rec := e.sendJSON(http.MethodPost, "/api/staff/activity/export", c, body); rec.Code != tt.want {
				t.Errorf("= %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

// FR-P4: the export checks its filters as GET /api/staff/activity does, plus the language; paging is not a filter.
func TestActivityExportValidation_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	c := e.session(e.newStaff("Team Lead", true))
	tests := []struct{ body, want string }{
		{`{"lang": "de"}`, `{"error":"validation","fields":{"lang":"invalid"}}`},
		{`{}`, `{"error":"validation","fields":{"lang":"invalid"}}`},
		{`{"lang": "en", "staff": "x", "action": ["Bad"], "from": "2003-06-12", "to": "2003-06-11", "tz": "Nowhere"}`,
			`{"error":"validation","fields":{"action":"invalid","staff":"invalid","to":"invalid","tz":"invalid"}}`},
		{`{"lang": "en", "ticket": 5}`, `{"error":"invalid_body"}`},              // filters are strings, as in the query
		{`{"lang": "en", "action": "login.failed"}`, `{"error":"invalid_body"}`}, // action is a list
		{`{"lang": "en", "page": "2"}`, `{"error":"invalid_body"}`},
		{``, `{"error":"invalid_body"}`},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			rec := e.sendJSON(http.MethodPost, "/api/staff/activity/export", c, tt.body)
			if rec.Code != http.StatusBadRequest || rec.Body.String() != tt.want+"\n" {
				t.Errorf("= %d %s, want 400 %s", rec.Code, rec.Body, tt.want)
			}
		})
	}
	if path, _ := g.last(); path != "" {
		t.Errorf("Gotenberg was called (%s) for a bad request", path)
	}
}
