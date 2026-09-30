package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// getQueue calls GET /api/staff/tickets with query and decodes the page.
func (e *testEnv) getQueue(query string, c *http.Cookie) queuePage {
	e.t.Helper()
	rec := e.withCookie(http.MethodGet, "/api/staff/tickets?"+query, c)
	var out queuePage
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("GET /api/staff/tickets?%s = %d %s", query, rec.Code, rec.Body)
	}
	return out
}

func ids(p queuePage) []int64 {
	out := []int64{}
	for _, it := range p.Items {
		out = append(out, it.ID)
	}
	return out
}

// T1.18: status, priority, assignee and search filters combine with AND; values within one filter with OR.
func TestQueue_FiltersCombine_T118(t *testing.T) {
	e := newAuthEnv(t)
	me := e.newStaff("Agent", true)
	other := e.newStaff("Agent", true)
	c := e.session(me)
	mark := fmt.Sprintf("qmark%d", time.Now().UnixNano())

	mk := func(status string, priority *string, assignee *int64) int64 {
		out := e.newTicket(func(in *createTicketInput) { in.CaseDetails = "Queue test ticket " + mark })
		err := e.db.Exec("UPDATE tickets SET status = ?, priority = ?, assignee_id = ? WHERE id = ?",
			status, priority, assignee, out.TicketID).Error
		if err != nil {
			t.Fatal(err)
		}
		return out.TicketID
	}
	high := "high"
	a := mk("new", nil, nil)
	b := mk("in_progress", &high, &me.ID)
	c2 := mk("in_progress", nil, &other.ID)
	d := mk("waiting", &high, &me.ID)

	tests := []struct {
		query string
		want  []int64 // newest first
	}{
		{"", []int64{d, c2, b, a}},
		{"&status=in_progress", []int64{c2, b}},
		{"&status=in_progress&status=waiting&priority=high", []int64{d, b}},
		{"&priority=none", []int64{c2, a}},
		{"&priority=high&priority=none", []int64{d, c2, b, a}},
		{"&assignee=me", []int64{d, b}},
		{"&assignee=none", []int64{a}},
		{"&assignee=" + strconv.FormatInt(other.ID, 10), []int64{c2}},
		{"&status=in_progress&assignee=me&priority=high", []int64{b}},
		{"&status=closed", []int64{}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := e.getQueue("q="+mark+tt.query, c)
			if !slices.Equal(ids(got), tt.want) || got.Total != int64(len(tt.want)) {
				t.Errorf("ids = %v total %d, want %v", ids(got), got.Total, tt.want)
			}
		})
	}

	t.Run("pagination", func(t *testing.T) {
		got := e.getQueue("q="+mark+"&page_size=3&page=2", c)
		if !slices.Equal(ids(got), []int64{a}) || got.Total != 4 || got.Page != 2 || got.PageSize != 3 {
			t.Errorf("page 2 = %v total %d page %d size %d, want [%d] of 4", ids(got), got.Total, got.Page, got.PageSize, a)
		}
	})

	t.Run("staff fields", func(t *testing.T) {
		got := e.getQueue("q="+mark+"&assignee=me&priority=high&status=in_progress&lang=th", c)
		it := got.Items[0]
		if it.Assignee == nil || it.Assignee.ID != me.ID || it.Priority == nil || *it.Priority != "high" ||
			it.EmployeeID == "" || it.Location.Building != e.loc.Building["th"] || !strings.Contains(it.Summary, mark) {
			t.Errorf("item = %+v", it)
		}
	})
}

// T1.18: Thai and Burmese phrases inside case details are found, and so are employee IDs and ticket numbers.
func TestQueue_SearchThaiBurmese_T118(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Viewer", true))
	th := e.newTicket(func(in *createTicketInput) {
		in.CaseDetails, in.Language = "เครื่องพิมพ์ชั้นสองไม่ทำงานตั้งแต่เช้า", "th"
	})
	my := e.newTicket(func(in *createTicketInput) {
		in.CaseDetails, in.Language = "ကွန်ပျူတာ ဖွင့်လို့ မရပါ၊ မီးလည်း မလာပါ", "my"
	})
	emp := fmt.Sprintf("QE%d", time.Now().UnixNano()%1_000_000_000)
	byEmp := e.newTicket(func(in *createTicketInput) { in.EmployeeID = emp })

	tests := []struct {
		name, q string
		want    int64
	}{
		{"thai phrase inside a word run", "ไม่ทำงาน", th.TicketID},
		{"burmese phrase", "ဖွင့်လို့ မရ", my.TicketID},
		{"employee id any case", strings.ToLower(emp), byEmp.TicketID},
		{"ticket number", "#" + strconv.FormatInt(byEmp.TicketID, 10), byEmp.TicketID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.getQueue("status=new&q="+url.QueryEscape(tt.q), c)
			if !slices.Contains(ids(got), tt.want) {
				t.Errorf("q=%q found %v, want %d among them", tt.q, ids(got), tt.want)
			}
		})
	}

	t.Run("like wildcards are literal", func(t *testing.T) {
		if got := e.getQueue("q="+url.QueryEscape("%_%"), c); got.Total != 0 {
			t.Errorf("q=%%_%% matched %d tickets, want 0", got.Total)
		}
	})
}

// FR-P1 ("click opens filtered queue"): each summary card's number equals the queue total behind its link, built as
// frontend/src/lib/components/ReportCards.svelte builds it. Open, Unassigned and Urgent open use the state as of
// the period end (as_of), New and Resolved the period on created_at or resolved_at; building and category scope all.
func TestQueueMatchesReportCards_FRP1(t *testing.T) {
	e := newAuthEnv(t)
	f := e.reportData()
	open := "status=new&status=in_progress&status=waiting"
	cards := []struct{ card, link string }{
		{"open", open + "&as_of=%[2]s"},
		{"unassigned", open + "&as_of=%[2]s&assignee=none"},
		{"urgent_open", open + "&as_of=%[2]s&priority=urgent"},
		{"new", "by=created&from=%[1]s&to=%[2]s"},
		{"resolved", "by=resolved&from=%[1]s&to=%[2]s"},
	}
	for _, scope := range []struct{ name, from, to, extra string }{
		{"building", "2020-03-01", "2020-03-10", "&tz=UTC"},
		{"category", "2020-03-01", "2020-03-10", fmt.Sprintf("&tz=UTC&category_id=%d", f.cat)},
		{"bangkok day", "2020-03-10", "2020-03-10", "&tz=Asia/Bangkok"},
	} {
		t.Run(scope.name, func(t *testing.T) {
			filters := scope.extra + "&building=" + url.QueryEscape(f.building)
			var report struct {
				Cards map[string]struct{ Value int64 }
			}
			e.getJSON(fmt.Sprintf("/api/staff/reports?from=%s&to=%s%s", scope.from, scope.to, filters), f.viewer, &report)
			for _, c := range cards {
				link := fmt.Sprintf(c.link, scope.from, scope.to) + filters
				if got, want := e.getQueue(link, f.viewer).Total, report.Cards[c.card].Value; got != want {
					t.Errorf("%s: queue ?%s total = %d, card = %d", c.card, link, got, want)
				}
			}
		})
	}
}

// FR-P1: the report filters on the queue are checked like the report's own (FR-P2).
func TestQueue_BadReportFilters_FRP1(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Agent", true))
	for _, tt := range []struct{ query, want string }{
		{"from=2020-13-01", `{"from":"invalid"}`},
		{"to=10-03-2020", `{"to":"invalid"}`},
		{"from=2020-03-10&to=2020-03-09", `{"to":"invalid"}`},
		{"as_of=2020-02-30", `{"as_of":"invalid"}`},
		{"by=updated&from=2020-03-01", `{"by":"invalid"}`},
		{"tz=Mars/Olympus&as_of=2020-03-01", `{"tz":"invalid"}`},
		{"category_id=0", `{"category_id":"invalid"}`},
		{"building=" + strings.Repeat("b", 201), `{"building":"too_long"}`},
	} {
		rec := e.withCookie(http.MethodGet, "/api/staff/tickets?"+tt.query, c)
		if want := `{"error":"validation","fields":` + tt.want + "}\n"; rec.Code != http.StatusBadRequest || rec.Body.String() != want {
			t.Errorf("%s = %d %s, want 400 %s", tt.query, rec.Code, rec.Body, want)
		}
	}
}

func TestQueue_BadFilters_T118(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Agent", true))
	for _, q := range []string{"status=bogus", "priority=extreme", "assignee=abc", "page=0", "page=1001", "page_size=101", strings.Repeat("status=new&", 11), "q=" + strings.Repeat("x", 201)} {
		if rec := e.withCookie(http.MethodGet, "/api/staff/tickets?"+q, c); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"validation"`) {
			t.Errorf("%s = %d %s, want 400 validation", q, rec.Code, rec.Body)
		}
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/tickets", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session = %d, want 401", rec.Code)
	}
}

// FR-R3: the API checks the permission itself; a role without ticket.view_all gets 403.
func TestQueue_NeedsViewPermission_FRR3(t *testing.T) {
	e := newAuthEnv(t)
	role := fmt.Sprintf("Test No View %d", time.Now().UnixNano())
	if err := e.db.Exec("INSERT INTO roles (name) VALUES (?)", role).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.db.Exec("DELETE FROM roles WHERE name = ?", role) })
	c := e.session(e.newStaff(role, true))

	if rec := e.withCookie(http.MethodGet, "/api/staff/tickets", c); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/staff/tickets without ticket.view_all = %d, want 403", rec.Code)
	}
}
