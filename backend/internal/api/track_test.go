package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// addStaffComments inserts one public reply and one internal note by a throwaway staff member.
func (e *testEnv) addStaffComments(ticketID int64) {
	e.t.Helper()
	var role models.Role
	if err := e.db.Where("name = ?", "Agent").First(&role).Error; err != nil {
		e.t.Fatal(err)
	}
	s := models.Staff{Name: "Agent Test", Username: "agent-" + strconv.FormatInt(ticketID, 10), PasswordChangedAt: time.Now(),
		RoleID: role.ID, Language: "en", IsActive: true}
	if err := e.db.Create(&s).Error; err != nil {
		e.t.Fatal(err)
	}
	// Cleanups run last-in first-out: this one runs first, so delete the ticket (and its comments) before the staff row.
	e.t.Cleanup(func() {
		e.db.Exec("DELETE FROM tickets WHERE id = ?", ticketID)
		e.db.Exec("DELETE FROM staff WHERE id = ?", s.ID)
	})
	for _, c := range []models.Comment{
		{TicketID: ticketID, AuthorStaffID: &s.ID, Body: "We are sending a technician."},
		{TicketID: ticketID, AuthorStaffID: &s.ID, Body: "SECRET internal note: user broke it", IsInternal: true},
	} {
		if err := e.db.Create(&c).Error; err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *testEnv) view(token string) (trackView, int) {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/api/track?lang=th", token, nil, "")
	var v trackView
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			e.t.Fatal(err)
		}
	}
	return v, rec.Code
}

// FR-G3, FR-G5: the tracking view shows status and public replies, never internal notes or guest identity.
func TestTrackView_FRG5(t *testing.T) {
	e := newTestEnv(t)
	c := e.newTicket()
	e.addStaffComments(c.TicketID)

	rec := e.do(http.MethodGet, "/api/track?lang=th", c.TrackingToken, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/track = %d: %s", rec.Code, rec.Body)
	}
	raw := rec.Body.String()
	for _, secret := range []string{"SECRET internal note", "is_internal", "Aye Aye", "E1001", "access_token", "Agent Test"} {
		if strings.Contains(raw, secret) {
			t.Errorf("response contains %q; guests must not see it: %s", secret, raw)
		}
	}
	v, _ := e.view(c.TrackingToken)
	if v.TicketID != c.TicketID || v.Status != models.StatusNew {
		t.Errorf("view = id %d status %q", v.TicketID, v.Status)
	}
	if len(v.Comments) != 1 || v.Comments[0].From != "staff" || v.Comments[0].Body != "We are sending a technician." {
		t.Errorf("comments = %+v, want the one public staff reply", v.Comments)
	}
	if v.Location.Building == "" || !strings.Contains(v.Location.Building, "อาคาร") {
		t.Errorf("location building = %q, want the Thai name", v.Location.Building)
	}

	for name, tok := range map[string]string{"wrong": "not-a-real-token", "empty": ""} {
		if _, code := e.view(tok); code != http.StatusNotFound {
			t.Errorf("%s token: code = %d, want 404", name, code)
		}
	}
}

func (e *testEnv) reply(token, body string) *bytesRecorder {
	b, _ := json.Marshal(map[string]string{"body": body})
	rec := e.do(http.MethodPost, "/api/track/comments", token, bytes.NewReader(b), "application/json")
	return &bytesRecorder{rec.Code, rec.Body.String()}
}

type bytesRecorder struct {
	code int
	body string
}

// FR-G3, FR-T6: a guest reply is public, audited, and reopens a waiting or resolved ticket.
func TestTrackReply_FRG3(t *testing.T) {
	e := newTestEnv(t)
	c := e.newTicket()

	if r := e.reply(c.TrackingToken, "Still broken after restart."); r.code != http.StatusCreated {
		t.Fatalf("reply = %d %s", r.code, r.body)
	}
	var got models.Comment
	if err := e.db.Where("ticket_id = ?", c.TicketID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.IsInternal || got.AuthorStaffID != nil {
		t.Errorf("guest comment = internal %v author %v, want public from guest", got.IsInternal, got.AuthorStaffID)
	}

	tests := []struct{ from, want string }{
		{models.StatusNew, models.StatusNew},
		{models.StatusInProgress, models.StatusInProgress},
		{models.StatusWaiting, models.StatusInProgress},
		{models.StatusResolved, models.StatusInProgress}, // reopen
	}
	for _, tt := range tests {
		e.setStatus(c.TicketID, tt.from)
		if r := e.reply(c.TrackingToken, "More details from me."); r.code != http.StatusCreated {
			t.Fatalf("reply from %s = %d %s", tt.from, r.code, r.body)
		}
		if v, _ := e.view(c.TrackingToken); v.Status != tt.want {
			t.Errorf("reply while %s: status = %s, want %s", tt.from, v.Status, tt.want)
		}
	}
	var audits int64
	e.db.Model(&models.AuditEntry{}).Where("ticket_id = ? AND action = 'ticket.status_changed' AND actor_type = 'guest' AND to_value = 'in_progress' AND from_value IN ('waiting', 'resolved')", c.TicketID).Count(&audits)
	if audits != 2 {
		t.Errorf("guest status_changed audits = %d, want 2 (waiting and resolved)", audits)
	}

	for name, body := range map[string]string{"empty": "  ", "too long": strings.Repeat("a", 5001), "bidi": "fine " + string(rune(0x202E)) + "text"} {
		if r := e.reply(c.TrackingToken, body); r.code != http.StatusBadRequest {
			t.Errorf("%s reply: code = %d, want 400", name, r.code)
		}
	}
	e.setStatus(c.TicketID, models.StatusClosed)
	if r := e.reply(c.TrackingToken, "Hello again please."); r.code != http.StatusConflict {
		t.Errorf("reply to closed ticket: code = %d, want 409", r.code)
	}
	if r := e.reply("wrong", "Hello there friend."); r.code != http.StatusNotFound {
		t.Errorf("wrong token: code = %d, want 404", r.code)
	}
}

// FR-G3, FR-T5: the guest confirms the fix, which closes a resolved ticket only.
func TestTrackConfirm_FRG3(t *testing.T) {
	e := newTestEnv(t)
	c := e.newTicket()
	confirm := func() int { return e.do(http.MethodPost, "/api/track/confirm", c.TrackingToken, nil, "").Code }

	if code := confirm(); code != http.StatusConflict {
		t.Errorf("confirm while new: code = %d, want 409", code)
	}
	e.setStatus(c.TicketID, models.StatusResolved)
	if code := confirm(); code != http.StatusOK {
		t.Fatalf("confirm while resolved: code = %d, want 200", code)
	}
	if v, _ := e.view(c.TrackingToken); v.Status != models.StatusClosed {
		t.Errorf("status after confirm = %s, want closed", v.Status)
	}
	var audits int64
	e.db.Model(&models.AuditEntry{}).Where("ticket_id = ? AND action = 'ticket.status_changed' AND from_value = 'resolved' AND to_value = 'closed'", c.TicketID).Count(&audits)
	if audits != 1 {
		t.Errorf("confirm audits = %d, want 1", audits)
	}
}

// NFR-5: evidence is served with a strict type, nosniff and a sandbox, and only to the ticket's own token.
func TestTrackFile_NFR5(t *testing.T) {
	e := newTestEnv(t)
	a, b := e.newTicket(), e.newTicket()
	photo := append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{7}, 500)...)
	rec := e.upload(a.TicketID, a.TrackingToken, "p.jpg", photo)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d", rec.Code)
	}
	var up struct{ ID int64 }
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	path := "/api/track/attachments/" + strconv.FormatInt(up.ID, 10)

	rec = e.do(http.MethodGet, path, a.TrackingToken, nil, "")
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), photo) {
		t.Fatalf("own file: code = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	for header, want := range map[string]string{
		"Content-Type":            "image/jpeg",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if rec := e.do(http.MethodGet, path, b.TrackingToken, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another guest's token: code = %d, want 404", rec.Code)
	}
	if v, _ := e.view(a.TrackingToken); len(v.Attachments) != 1 || v.Attachments[0].MediaType != "image" {
		t.Errorf("view attachments = %+v", v.Attachments)
	}
}
