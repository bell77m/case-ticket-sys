package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"ticket-app/internal/models"
)

// T1.19, FR-T6: staff see the whole thread, internal notes included, with authors; never the token hash.
func TestStaffTicket_Detail_T119(t *testing.T) {
	e := newAuthEnv(t)
	agent := e.newStaff("Agent", true)
	c := e.session(e.newStaff("Viewer", true))
	tk := e.newTicket()
	path := "/api/staff/tickets/" + strconv.FormatInt(tk.TicketID, 10)

	body, _ := json.Marshal(map[string]string{"body": "Still broken after restart."})
	if rec := e.do(http.MethodPost, "/api/track/comments", tk.TrackingToken, bytes.NewReader(body), "application/json"); rec.Code != http.StatusCreated {
		t.Fatalf("guest reply = %d %s", rec.Code, rec.Body)
	}
	for _, cm := range []models.Comment{
		{TicketID: tk.TicketID, AuthorStaffID: &agent.ID, Body: "Checking the toner.", IsInternal: false},
		{TicketID: tk.TicketID, AuthorStaffID: &agent.ID, Body: "Vendor ticket 4411.", IsInternal: true},
	} {
		if err := e.db.Create(&cm).Error; err != nil {
			t.Fatal(err)
		}
	}
	if rec := e.upload(tk.TicketID, tk.TrackingToken, "a.jpg", append(append([]byte{}, jpegHead...), make([]byte, 100)...)); rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}

	rec := e.withCookie(http.MethodGet, path+"?lang=th", c)
	var got staffTicket
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "access_token") || strings.Contains(rec.Body.String(), "hash") {
		t.Errorf("staff view leaks the tracking token hash: %s", rec.Body)
	}
	if got.ID != tk.TicketID || got.GuestName == "" || got.EmployeeID == "" || got.Location.Building != e.loc.Building["th"] {
		t.Errorf("ticket fields = %+v", got)
	}
	if len(got.Comments) != 3 {
		t.Fatalf("comments = %+v, want guest reply, public reply, internal note", got.Comments)
	}
	guest, public, note := got.Comments[0], got.Comments[1], got.Comments[2]
	if guest.Author != nil || guest.Internal {
		t.Errorf("guest comment = %+v, want no author, public", guest)
	}
	if public.Author == nil || public.Author.ID != agent.ID || public.Internal {
		t.Errorf("public reply = %+v, want author %d, public", public, agent.ID)
	}
	if !note.Internal || note.Body != "Vendor ticket 4411." {
		t.Errorf("internal note = %+v, want internal", note)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].MediaType != "image" {
		t.Fatalf("attachments = %+v, want one image", got.Attachments)
	}

	// Evidence is served with the same safe headers as on the tracking page (NFR-5).
	file := e.withCookie(http.MethodGet, path+"/attachments/"+strconv.FormatInt(got.Attachments[0].ID, 10), c)
	if file.Code != http.StatusOK || file.Header().Get("Content-Type") != "image/jpeg" || file.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("file = %d %v", file.Code, file.Header())
	}

	other := e.newTicket()
	wrong := "/api/staff/tickets/" + strconv.FormatInt(other.TicketID, 10) + "/attachments/" + strconv.FormatInt(got.Attachments[0].ID, 10)
	if rec := e.withCookie(http.MethodGet, wrong, c); rec.Code != http.StatusNotFound {
		t.Errorf("file through another ticket = %d, want 404", rec.Code)
	}
}

func TestStaffTicket_NotFoundAndAuth_T119(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Agent", true))
	for _, p := range []string{"/api/staff/tickets/999999999", "/api/staff/tickets/abc"} {
		if rec := e.withCookie(http.MethodGet, p, c); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
	tk := e.newTicket()
	if rec := e.withCookie(http.MethodGet, "/api/staff/tickets/"+strconv.FormatInt(tk.TicketID, 10), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session = %d, want 401", rec.Code)
	}
}

// FR-L3: the detail shows the ticket's own timeline, oldest first, with who did each step and display values.
// Staff names survive deactivation; no IP address or target ever leaves the API.
func TestStaffTicket_Timeline_FRL3(t *testing.T) {
	e := newAuthEnv(t)
	rename := func(st *models.Staff, name string) {
		st.Name = name + strconv.FormatInt(st.ID, 10)
		if err := e.db.Model(st).Update("name", st.Name).Error; err != nil {
			t.Fatal(err)
		}
	}
	agent, lead := e.newStaff("Agent", true), e.newStaff("Team Lead", true)
	rename(&agent, "FR-L3 Agent ")
	rename(&lead, "FR-L3 Lead ")
	agentC, leadC := e.session(agent), e.session(lead)

	var cats []models.Category
	if err := e.db.Where("is_active").Order("id").Limit(2).Find(&cats).Error; err != nil || len(cats) < 2 {
		t.Fatalf("need 2 active categories: %v", err)
	}
	tk := e.newTicket()
	path := ticketPath(tk.TicketID)
	e.db.Exec("UPDATE tickets SET category_id = ? WHERE id = ?", cats[0].ID, tk.TicketID) // a starting value, not audited

	steps := []struct {
		method, path string
		c            *http.Cookie
		body         string
	}{
		{http.MethodPatch, path, agentC, `{"status": "in_progress"}`},
		{http.MethodPut, path + "/assignee", leadC, `{"assignee_id": ` + strconv.FormatInt(agent.ID, 10) + `}`},
		{http.MethodPatch, path, leadC, `{"category_id": ` + strconv.FormatInt(cats[1].ID, 10) + `}`},
		{http.MethodPatch, path, leadC, `{"status": "resolved"}`},
	}
	for _, s := range steps {
		if rec := e.sendJSON(s.method, s.path, s.c, s.body); rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s %s = %d %s", s.method, s.path, s.body, rec.Code, rec.Body)
		}
	}
	if rec := e.do(http.MethodPost, "/api/track/confirm", tk.TrackingToken, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("guest confirm = %d %s", rec.Code, rec.Body)
	}
	e.db.Exec("UPDATE staff SET is_active = false WHERE id = ?", agent.ID) // left staff keep their name in history

	rec := e.withCookie(http.MethodGet, path+"?lang=th", e.session(e.newStaff("Viewer", true)))
	var got staffTicket
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	if got.CategoryID == nil || *got.CategoryID != cats[1].ID {
		t.Errorf("category_id = %v, want %d", got.CategoryID, cats[1].ID)
	}
	for _, leak := range []string{`"ip`, "192.0.2.1", `"target"`, tk.TrackingToken} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("detail contains %q: %s", leak, rec.Body)
		}
	}

	want := []struct{ action, actorType, actor, from, to string }{
		{"ticket.created", models.ActorGuest, "", "", "location:" + strconv.FormatInt(e.loc.ID, 10)},
		{"ticket.status_changed", models.ActorStaff, agent.Name, "new", "in_progress"},
		{"ticket.assigned", models.ActorStaff, lead.Name, "", agent.Name},
		{"ticket.category_changed", models.ActorStaff, lead.Name, pickName(cats[0].Name, "th"), pickName(cats[1].Name, "th")},
		{"ticket.status_changed", models.ActorStaff, lead.Name, "in_progress", "resolved"},
		{"ticket.status_changed", models.ActorGuest, "", "resolved", "closed"},
	}
	if len(got.Timeline) != len(want) {
		t.Fatalf("timeline = %+v, want %d events", got.Timeline, len(want))
	}
	for i, w := range want {
		g := got.Timeline[i]
		if g.Action != w.action || g.Actor.Type != w.actorType || g.Actor.Name != w.actor || g.From != w.from || g.To != w.to || g.CreatedAt.IsZero() {
			t.Errorf("timeline[%d] = %+v, want %+v", i, g, w)
		}
	}
}

// T2.06, FR-T3: the assignee picker lists active staff only; a role without ticket.assign is refused.
func TestStaffAssignees_FRT3(t *testing.T) {
	e := newAuthEnv(t)
	on, off := e.newStaff("Agent", true), e.newStaff("Agent", false)
	rec := e.withCookie(http.MethodGet, "/api/staff/assignees", e.session(on))
	var got []namedItem
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET assignees = %d %s", rec.Code, rec.Body)
	}
	listed := map[int64]bool{}
	for _, it := range got {
		listed[it.ID] = true
	}
	if !listed[on.ID] || listed[off.ID] {
		t.Errorf("assignees = %+v, want %d listed and %d not", got, on.ID, off.ID)
	}
	if rec := e.withCookie(http.MethodGet, "/api/staff/assignees", e.session(e.newStaff("Viewer", true))); rec.Code != http.StatusForbidden {
		t.Errorf("Viewer = %d, want 403", rec.Code)
	}
}
