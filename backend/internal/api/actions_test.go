package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// sendJSON sends a JSON body with the session cookie.
func (e *testEnv) sendJSON(method, path string, c *http.Cookie, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// auditSince returns the audit rows of a ticket written after row id since, oldest first.
func (e *testEnv) auditSince(ticketID, since int64) []models.AuditEntry {
	e.t.Helper()
	var rows []models.AuditEntry
	if err := e.db.Where("ticket_id = ? AND id > ?", ticketID, since).Order("id").Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *testEnv) lastAuditID() int64 {
	var id int64
	e.db.Raw("SELECT coalesce(max(id), 0) FROM audit_log").Scan(&id)
	return id
}

func (e *testEnv) ticket(id int64) models.Ticket {
	e.t.Helper()
	var t models.Ticket
	if err := e.db.First(&t, id).Error; err != nil {
		e.t.Fatal(err)
	}
	return t
}

func ticketPath(id int64) string { return "/api/staff/tickets/" + strconv.FormatInt(id, 10) }

// FR-T5: staff may move a ticket from any status to any other, closed included. Each change is audited with old
// and new value; saving the current status writes nothing. resolved_at is set on Resolved, kept (or set) on
// Closed, and cleared when the ticket reopens.
func TestStaffStatus_FRT5(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.newStaff("Agent", true)
	c := e.session(agent)
	tk := e.newTicket()
	path := ticketPath(tk.TicketID)

	all := []string{models.StatusNew, models.StatusInProgress, models.StatusWaiting, models.StatusResolved, models.StatusClosed}
	done := func(s string) bool { return s == models.StatusResolved || s == models.StatusClosed }
	for _, from := range all {
		for _, to := range all {
			t.Run(from+"→"+to, func(t *testing.T) {
				e.t = t
				resolvedAt := "NULL"
				if done(from) {
					resolvedAt = "now() - interval '1 day'"
				}
				if err := e.db.Exec("UPDATE tickets SET status = ?, resolved_at = "+resolvedAt+" WHERE id = ?", from, tk.TicketID).Error; err != nil {
					t.Fatal(err)
				}
				before := e.lastAuditID()
				old := e.ticket(tk.TicketID).ResolvedAt

				rec := e.sendJSON(http.MethodPatch, path, c, fmt.Sprintf(`{"status": %q}`, to))
				if rec.Code != http.StatusNoContent {
					t.Fatalf("PATCH = %d %s, want 204", rec.Code, rec.Body)
				}
				got := e.ticket(tk.TicketID)
				audits := e.auditSince(tk.TicketID, before)
				if got.Status != to {
					t.Errorf("status = %s, want %s", got.Status, to)
				}
				if (got.ResolvedAt != nil) != done(to) {
					t.Errorf("resolved_at = %v, want set = %v", got.ResolvedAt, done(to))
				}
				// Closing a resolved ticket, or saving the same status, keeps the resolution time.
				if keep := from == to || (from == models.StatusResolved && to == models.StatusClosed); keep && old != nil &&
					(got.ResolvedAt == nil || !got.ResolvedAt.Equal(*old)) {
					t.Errorf("resolved_at = %v, want kept %v", got.ResolvedAt, old)
				}
				if from == to {
					if len(audits) != 0 {
						t.Errorf("unchanged status wrote %d audit rows", len(audits))
					}
					return
				}
				if len(audits) != 1 {
					t.Fatalf("audit rows = %d, want 1", len(audits))
				}
				a := audits[0]
				if a.Action != "ticket.status_changed" || optText(a.FromValue) != from || optText(a.ToValue) != to ||
					a.ActorType != models.ActorStaff || a.ActorStaffID == nil || *a.ActorStaffID != agent.ID {
					t.Errorf("audit = %s %q→%q by %s %v, want status_changed %s→%s by staff %d",
						a.Action, optText(a.FromValue), optText(a.ToValue), a.ActorType, a.ActorStaffID, from, to, agent.ID)
				}
			})
		}
	}
}

// FR-T3: staff set priority and category; each change is audited with old and new value, a repeat is not.
// A body with one bad field changes nothing.
func TestStaffTriage_FRT3(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Team Lead", true))
	tk := e.newTicket()
	path := ticketPath(tk.TicketID)

	var cats []models.Category
	if err := e.db.Where("is_active").Order("id").Limit(2).Find(&cats).Error; err != nil || len(cats) < 2 {
		t.Fatalf("need 2 active categories: %v", err)
	}
	hw, sw := strconv.FormatInt(cats[0].ID, 10), strconv.FormatInt(cats[1].ID, 10)
	inactive := models.Category{Name: models.Names{"en": "T2.04 inactive probe"}, IsActive: false}
	if err := e.db.Create(&inactive).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.db.Delete(&inactive) })
	gone := strconv.FormatInt(inactive.ID, 10)

	type row struct{ action, from, to string }
	tests := []struct {
		name   string
		body   string
		code   int
		audits []row
	}{
		{"priority set", `{"priority": "high"}`, 204, []row{{"ticket.priority_changed", "", "high"}}},
		{"priority unchanged", `{"priority": "high"}`, 204, nil},
		{"priority changed", `{"priority": "urgent"}`, 204, []row{{"ticket.priority_changed", "high", "urgent"}}},
		{"priority invalid", `{"priority": "critical"}`, 400, nil},
		{"category set", `{"category_id": ` + hw + `}`, 204, []row{{"ticket.category_changed", "", hw}}},
		{"category unchanged", `{"category_id": ` + hw + `}`, 204, nil},
		{"category changed", `{"category_id": ` + sw + `}`, 204, []row{{"ticket.category_changed", hw, sw}}},
		{"category inactive", `{"category_id": ` + gone + `}`, 400, nil},
		{"category unknown", `{"category_id": 999999999}`, 400, nil},
		{"mixed, bad priority", `{"status": "in_progress", "priority": "bogus", "category_id": ` + hw + `}`, 400, nil},
		{"mixed, bad category", `{"status": "in_progress", "priority": "low", "category_id": ` + gone + `}`, 400, nil},
		{"unknown status", `{"status": "done"}`, 400, nil},
		{"empty body", `{}`, 400, nil},
		{"unknown field", `{"assignee_id": 1}`, 400, nil},
		{"all three", `{"status": "in_progress", "priority": "low", "category_id": ` + hw + `}`, 204, []row{
			{"ticket.priority_changed", "urgent", "low"}, {"ticket.category_changed", sw, hw}, {"ticket.status_changed", "new", "in_progress"}}},
	}
	for _, tt := range tests {
		before, was := e.lastAuditID(), e.ticket(tk.TicketID)
		rec := e.sendJSON(http.MethodPatch, path, c, tt.body)
		if rec.Code != tt.code {
			t.Errorf("%s: PATCH = %d %s, want %d", tt.name, rec.Code, rec.Body, tt.code)
		}
		var got []row
		for _, a := range e.auditSince(tk.TicketID, before) {
			got = append(got, row{a.Action, optText(a.FromValue), optText(a.ToValue)})
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.audits) {
			t.Errorf("%s: audits = %v, want %v", tt.name, got, tt.audits)
		}
		if now := e.ticket(tk.TicketID); tt.code != 204 && (now.Status != was.Status || optText(now.Priority) != optText(was.Priority) || optText(now.CategoryID) != optText(was.CategoryID)) {
			t.Errorf("%s: rejected request changed the ticket: %s %s %v", tt.name, now.Status, optText(now.Priority), now.CategoryID)
		}
	}
	if got := e.ticket(tk.TicketID); got.Status != models.StatusInProgress || optText(got.Priority) != "low" || got.CategoryID == nil || *got.CategoryID != cats[0].ID {
		t.Errorf("final ticket = %s %s %v", got.Status, optText(got.Priority), got.CategoryID)
	}
}

// FR-T3 (assignee): an Agent may assign only themselves; other roles with ticket.assign assign any active staff.
// Assigning is audited with old and new staff IDs and does not change the status.
func TestStaffAssign_FRT3(t *testing.T) {
	e := newAuthEnv(t)
	lead, agent, other, off := e.newStaff("Team Lead", true), e.newStaff("Agent", true), e.newStaff("Agent", true), e.newStaff("Agent", false)
	leadC, agentC := e.session(lead), e.session(agent)
	tk := e.newTicket()
	path := ticketPath(tk.TicketID) + "/assignee"
	id := func(s models.Staff) string { return strconv.FormatInt(s.ID, 10) }

	tests := []struct {
		name     string
		c        *http.Cookie
		body     string
		code     int
		from, to string // wanted ticket.assigned row; both "" = no row
		assignee string
	}{
		{"lead assigns other", leadC, `{"assignee_id": ` + id(other) + `}`, 204, "", id(other), id(other)},
		{"lead repeats", leadC, `{"assignee_id": ` + id(other) + `}`, 204, "", "", id(other)},
		{"agent takes it", agentC, `{"assignee_id": ` + id(agent) + `}`, 204, id(other), id(agent), id(agent)},
		{"agent assigns other", agentC, `{"assignee_id": ` + id(other) + `}`, 403, "", "", id(agent)},
		{"agent unassigns", agentC, `{"assignee_id": null}`, 403, "", "", id(agent)},
		{"inactive assignee", leadC, `{"assignee_id": ` + id(off) + `}`, 400, "", "", id(agent)},
		{"unknown assignee", leadC, `{"assignee_id": 999999999}`, 400, "", "", id(agent)},
		{"missing field", leadC, `{}`, 400, "", "", id(agent)},
		{"not a number", leadC, `{"assignee_id": "` + id(other) + `"}`, 400, "", "", id(agent)},
		{"lead unassigns", leadC, `{"assignee_id": null}`, 204, id(agent), "", ""},
	}
	for _, tt := range tests {
		before := e.lastAuditID()
		rec := e.sendJSON(http.MethodPut, path, tt.c, tt.body)
		if rec.Code != tt.code {
			t.Errorf("%s: PUT = %d %s, want %d", tt.name, rec.Code, rec.Body, tt.code)
		}
		if tt.code == 403 && !strings.Contains(rec.Body.String(), `"ticket.assign_self_only"`) {
			t.Errorf("%s: body = %s, want ticket.assign_self_only", tt.name, rec.Body)
		}
		audits := e.auditSince(tk.TicketID, before)
		switch {
		case tt.from == "" && tt.to == "":
			if len(audits) != 0 {
				t.Errorf("%s: wrote %d audit rows, want none", tt.name, len(audits))
			}
		case len(audits) != 1 || audits[0].Action != "ticket.assigned" || optText(audits[0].FromValue) != tt.from || optText(audits[0].ToValue) != tt.to:
			t.Errorf("%s: audits = %+v, want one ticket.assigned %q→%q", tt.name, audits, tt.from, tt.to)
		}
		got, assignee := e.ticket(tk.TicketID), ""
		if got.AssigneeID != nil {
			assignee = strconv.FormatInt(*got.AssigneeID, 10)
		}
		if assignee != tt.assignee {
			t.Errorf("%s: assignee = %q, want %q", tt.name, assignee, tt.assignee)
		}
		if got.Status != models.StatusNew {
			t.Errorf("%s: status = %s, want new (assigning does not change it)", tt.name, got.Status)
		}
	}
}

// FR-T3, FR-T5: both action routes answer 404 ticket.not_found for an unknown ticket.
func TestStaffActions_NotFound_FRT3(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Team Lead", true))
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPatch, "/api/staff/tickets/999999999", `{"priority": "low"}`},
		{http.MethodPatch, "/api/staff/tickets/abc", `{"priority": "low"}`},
		{http.MethodPut, "/api/staff/tickets/999999999/assignee", `{"assignee_id": null}`},
	} {
		rec := e.sendJSON(r.method, r.path, c, r.body)
		var body struct{ Error string }
		if rec.Code != http.StatusNotFound || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error != "ticket.not_found" {
			t.Errorf("%s %s = %d %s, want 404 ticket.not_found", r.method, r.path, rec.Code, rec.Body)
		}
	}
}

// FR-T6: staff add a public reply or an internal note. Each is audited with its visibility, keeps the status,
// and comes back in the staff detail shape. The first public reply sets first_response_at; nothing later moves it.
func TestStaffComment_FRT6(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.newStaff("Agent", true)
	c := e.session(agent)
	tk := e.newTicket()
	path := ticketPath(tk.TicketID) + "/comments"

	tests := []struct {
		name     string
		body     string
		wantBody string
		internal bool
		firstSet bool // first_response_at is set after the call
	}{
		{"internal note on a fresh ticket", `{"body": "Vendor ticket 4411.", "internal": true}`, "Vendor ticket 4411.", true, false},
		{"first public reply", `{"body": "  Restart the printer.\r\nThen try again.  "}`, "Restart the printer.\nThen try again.", false, true},
		{"second public reply", `{"body": "Any luck?", "internal": false}`, "Any luck?", false, true},
		{"internal note later", `{"body": "Waiting on the guest.", "internal": true}`, "Waiting on the guest.", true, true},
	}
	var first *time.Time
	for _, tt := range tests {
		before := e.lastAuditID()
		rec := e.sendJSON(http.MethodPost, path, c, tt.body)
		var got staffComment
		if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("%s: POST = %d %s, want 201", tt.name, rec.Code, rec.Body)
		}
		if got.ID == 0 || got.Body != tt.wantBody || got.Internal != tt.internal || got.CreatedAt.IsZero() ||
			got.Author == nil || got.Author.ID != agent.ID || got.Author.Name != agent.Name {
			t.Errorf("%s: comment = %+v, want body %q internal %v by %d", tt.name, got, tt.wantBody, tt.internal, agent.ID)
		}

		want := "public"
		if tt.internal {
			want = "internal"
		}
		audits := e.auditSince(tk.TicketID, before)
		if len(audits) != 1 || audits[0].Action != "comment.added" || optText(audits[0].ToValue) != want ||
			audits[0].ActorStaffID == nil || *audits[0].ActorStaffID != agent.ID {
			t.Errorf("%s: audits = %+v, want one comment.added %q by staff %d", tt.name, audits, want, agent.ID)
		}

		now := e.ticket(tk.TicketID)
		if now.Status != models.StatusNew {
			t.Errorf("%s: status = %s, want new (a comment does not change it)", tt.name, now.Status)
		}
		switch {
		case (now.FirstResponseAt != nil) != tt.firstSet:
			t.Errorf("%s: first_response_at = %v, want set = %v", tt.name, now.FirstResponseAt, tt.firstSet)
		case first != nil && !now.FirstResponseAt.Equal(*first):
			t.Errorf("%s: first_response_at moved from %v to %v", tt.name, *first, *now.FirstResponseAt)
		}
		first = now.FirstResponseAt
	}
}

// FR-T6: a refused staff comment answers with an error code and writes no comment and no audit row.
func TestStaffComment_Rejected_FRT6(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Agent", true))
	open, closed := e.newTicket(), e.newTicket()
	e.setStatus(closed.TicketID, models.StatusClosed)
	before := e.lastAuditID()

	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	tests := []struct {
		name, ticket, body string
		code               int
		err, field         string // error code; fields.body for "validation"
	}{
		{"closed ticket", id(closed.TicketID), `{"body": "Hello?"}`, 409, "ticket.closed", ""},
		{"empty body", id(open.TicketID), `{"body": ""}`, 400, "validation", "required"},
		{"whitespace body", id(open.TicketID), `{"body": " \r\n\t ", "internal": true}`, 400, "validation", "required"},
		{"missing body", id(open.TicketID), `{"internal": true}`, 400, "validation", "required"},
		{"too long", id(open.TicketID), `{"body": "` + strings.Repeat("a", 5001) + `"}`, 400, "validation", "too_long"},
		{"unknown field", id(open.TicketID), `{"body": "Hi", "public": true}`, 400, "invalid_body", ""},
		{"not JSON", id(open.TicketID), `body=Hi`, 400, "invalid_body", ""},
		{"unknown ticket", "999999999", `{"body": "Hi"}`, 404, "ticket.not_found", ""},
		{"bad ticket id", "abc", `{"body": "Hi"}`, 404, "ticket.not_found", ""},
	}
	for _, tt := range tests {
		rec := e.sendJSON(http.MethodPost, "/api/staff/tickets/"+tt.ticket+"/comments", c, tt.body)
		var got struct {
			Error  string
			Fields map[string]string
		}
		if rec.Code != tt.code || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Error != tt.err || got.Fields["body"] != tt.field {
			t.Errorf("%s: POST = %d %s, want %d %s %s", tt.name, rec.Code, rec.Body, tt.code, tt.err, tt.field)
		}
	}

	var n int64
	e.db.Model(&models.Comment{}).Where("ticket_id IN ?", []int64{open.TicketID, closed.TicketID}).Count(&n)
	if n != 0 {
		t.Errorf("refused comments stored %d rows", n)
	}
	if a := append(e.auditSince(open.TicketID, before), e.auditSince(closed.TicketID, before)...); len(a) != 0 {
		t.Errorf("refused comments wrote audit rows: %+v", a)
	}
}

// FR-G5, FR-T6: the guest tracking view carries the staff's public reply but no trace of the internal note;
// the staff detail shows both, the note marked internal.
func TestStaffComment_GuestView_FRG5(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Agent", true))
	tk := e.newTicket()
	path := ticketPath(tk.TicketID)

	const public, secret = "A technician is on the way.", "Vendor ticket 4411, user dropped it."
	var note staffComment
	for _, body := range []string{`{"body": "` + public + `"}`, `{"body": "` + secret + `", "internal": true}`} {
		rec := e.sendJSON(http.MethodPost, path+"/comments", c, body)
		if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &note) != nil {
			t.Fatalf("POST %s = %d %s", body, rec.Code, rec.Body)
		}
	}

	rec := e.do(http.MethodGet, "/api/track", tk.TrackingToken, nil, "")
	raw := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(raw, public) {
		t.Fatalf("GET /api/track = %d %s, want the public reply", rec.Code, raw)
	}
	for _, leak := range []string{secret, "Vendor", "internal", "Test Staff"} {
		if strings.Contains(raw, leak) {
			t.Errorf("guest view contains %q: %s", leak, raw)
		}
	}
	v, _ := e.view(tk.TrackingToken)
	if len(v.Comments) != 1 || v.Comments[0].ID == note.ID || v.Comments[0].From != "staff" {
		t.Errorf("guest comments = %+v, want only the public staff reply", v.Comments)
	}

	rec = e.withCookie(http.MethodGet, path, c)
	var got staffTicket
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	if len(got.Comments) != 2 || got.Comments[0].Body != public || got.Comments[0].Internal ||
		got.Comments[1].ID != note.ID || got.Comments[1].Body != secret || !got.Comments[1].Internal {
		t.Errorf("staff comments = %+v, want the public reply then the internal note", got.Comments)
	}
}
