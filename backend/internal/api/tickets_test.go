package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

func validInput() createTicketInput {
	return createTicketInput{
		GuestName:   "Aye Aye",
		EmployeeID:  "E1001",
		LocationID:  1,
		CaseDetails: "The printer on line 2 jams every morning.",
		Language:    "my",
	}
}

// FR-G1, FR-T2, FR-T4 and the guest form rules in docs/REQUIREMENTS.md §2.
func TestValidateCreateTicket_FRG1(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*createTicketInput)
		field string // "" = valid
	}{
		{"valid", func(*createTicketInput) {}, ""},
		{"name missing", func(in *createTicketInput) { in.GuestName = "  " }, "guest_name"},
		{"name 100 runes ok", func(in *createTicketInput) { in.GuestName = strings.Repeat("မ", 100) }, ""},
		{"name 101 runes", func(in *createTicketInput) { in.GuestName = strings.Repeat("မ", 101) }, "guest_name"},
		{"employee id missing", func(in *createTicketInput) { in.EmployeeID = "" }, "employee_id"},
		{"employee id 21", func(in *createTicketInput) { in.EmployeeID = strings.Repeat("9", 21) }, "employee_id"},
		{"location missing", func(in *createTicketInput) { in.LocationID = 0 }, "location_id"},
		{"details 9 runes", func(in *createTicketInput) { in.CaseDetails = "ไม่ทำงาน!" }, "case_details"},
		{"details 10 runes ok", func(in *createTicketInput) { in.CaseDetails = "0123456789" }, ""},
		{"details 5001", func(in *createTicketInput) { in.CaseDetails = strings.Repeat("a", 5001) }, "case_details"},
		{"name only zero-width", func(in *createTicketInput) { in.GuestName = "\u200b\u200b" }, "guest_name"},
		{"name with bidi override", func(in *createTicketInput) { in.GuestName = "Aye\u202eeyA" }, "guest_name"},
		{"name with NUL", func(in *createTicketInput) { in.GuestName = "Aye\x00" }, "guest_name"},
		{"burmese name with ZWNJ ok", func(in *createTicketInput) { in.GuestName = "အေး\u200cအေး" }, ""},
		{"employee id with zero-width", func(in *createTicketInput) { in.EmployeeID = "E10\u200b01" }, "employee_id"},
		{"details with newline and tab ok", func(in *createTicketInput) { in.CaseDetails = "Line one\n\tline two" }, ""},
		{"details with bidi isolate", func(in *createTicketInput) { in.CaseDetails = "Printer \u2067jam\u2069 again" }, "case_details"},
		{"details with bell", func(in *createTicketInput) { in.CaseDetails = "Printer jam \a again" }, "case_details"},
		{"language unknown", func(in *createTicketInput) { in.Language = "fr" }, "language"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validInput()
			tt.edit(&in)
			errs := in.validate()
			if tt.field == "" && len(errs) > 0 {
				t.Fatalf("errors = %v, want none", errs)
			}
			if tt.field != "" && errs[tt.field] == "" {
				t.Fatalf("errors = %v, want one for %s", errs, tt.field)
			}
		})
	}
}

// FR-T2: summary is the first 80 characters of case details, on one line.
func TestSummary_FRT2(t *testing.T) {
	long := strings.Repeat("ก", 100)
	if got := summarize(long); len([]rune(got)) != 80 {
		t.Errorf("summary runes = %d, want 80", len([]rune(got)))
	}
	if got := summarize("  Line one\nline two  "); got != "Line one line two" {
		t.Errorf("summary = %q", got)
	}
}

// NFR-2, FR-L1, FR-T3: create stores only the token hash, audits in the same transaction,
// and rejects fields guests may not set.
func TestCreateTicketHandler_NFR2(t *testing.T) {
	e := newTestEnv(t)
	db, loc, mux := e.db, e.loc, e.mux
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/tickets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		return rec
	}

	in := validInput()
	in.LocationID = loc.ID
	body, _ := json.Marshal(in)
	rec := post(string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	var out createTicketOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM tickets WHERE id = ?", out.TicketID) })
	if len(out.TrackingToken) < 43 {
		t.Fatalf("token %q too short for 32 random bytes", out.TrackingToken)
	}

	var got models.Ticket
	if err := db.First(&got, out.TicketID).Error; err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(out.TrackingToken))
	if !bytes.Equal(got.AccessTokenHash, sum[:]) {
		t.Error("stored hash is not SHA-256 of the returned token")
	}
	var rawHits int64
	db.Raw(`SELECT count(*) FROM tickets t WHERE t.id = ? AND position(? in row_to_json(t)::text) > 0`,
		out.TicketID, out.TrackingToken).Scan(&rawHits)
	if rawHits != 0 {
		t.Error("raw tracking token found in the tickets row")
	}
	if got.Status != models.StatusNew || got.Priority != nil || got.CategoryID != nil {
		t.Errorf("ticket = status %q priority %v category %v, want new/nil/nil", got.Status, got.Priority, got.CategoryID)
	}

	var audits int64
	db.Model(&models.AuditEntry{}).Where("ticket_id = ? AND action = 'ticket.created' AND actor_type = 'guest'", out.TicketID).Count(&audits)
	if audits != 1 {
		t.Errorf("ticket.created audit rows = %d, want 1", audits)
	}

	for name, bad := range map[string]string{
		"guest sets priority (FR-T3)": `{"guest_name":"A","employee_id":"E1","location_id":` + itoa(loc.ID) + `,"case_details":"0123456789","language":"en","priority":"urgent"}`,
		"unknown location":            `{"guest_name":"A","employee_id":"E1","location_id":999999999,"case_details":"0123456789","language":"en"}`,
		"not json":                    `guest_name=A`,
	} {
		if rec := post(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// NFR-3: 5 tickets per IP per 10 minutes. The 6th gets 429 ticket.rate_limited and stores nothing; another IP still gets in.
func TestGuestRateLimit_NFR3(t *testing.T) {
	e := newTestEnv(t)
	mux := http.NewServeMux()
	(&Server{DB: e.db, Sessions: e.sessions}).Routes(mux) // GuestTicketLimit 0 means the default 5
	in := validInput()
	in.LocationID = e.loc.ID
	in.GuestName = fmt.Sprintf("Rate %d", time.Now().UnixNano()) // finds this test's rows among other packages' tickets
	body, _ := json.Marshal(in)
	t.Cleanup(func() { e.db.Exec("DELETE FROM tickets WHERE guest_name = ?", in.GuestName) })

	a, b := randomIP(), randomIP()
	steps := []struct {
		ip   string
		want int
		rows int64 // this test's tickets after the step
	}{
		{a, http.StatusCreated, 1}, {a, http.StatusCreated, 2}, {a, http.StatusCreated, 3},
		{a, http.StatusCreated, 4}, {a, http.StatusCreated, 5},
		{a, http.StatusTooManyRequests, 5},
		{b, http.StatusCreated, 6},
	}
	for i, st := range steps {
		req := httptest.NewRequest(http.MethodPost, "/api/tickets", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = st.ip + ":40000"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != st.want {
			t.Fatalf("request %d from %s = %d %s, want %d", i+1, st.ip, rec.Code, rec.Body, st.want)
		}
		if code, _ := errorBody(rec); st.want == http.StatusTooManyRequests && code != "ticket.rate_limited" {
			t.Errorf("request %d code = %q, want ticket.rate_limited", i+1, code)
		}
		var n int64
		e.db.Model(&models.Ticket{}).Where("guest_name = ?", in.GuestName).Count(&n)
		if n != st.rows {
			t.Fatalf("after request %d: %d tickets, want %d", i+1, n, st.rows)
		}
	}

	// Redis down: refuse the ticket rather than let guests past the limit.
	closed := redis.NewClient(&redis.Options{})
	_ = closed.Close()
	rec := httptest.NewRecorder()
	(&Server{DB: e.db, Sessions: &auth.Sessions{Redis: closed}}).createTicket(rec, httptest.NewRequest(http.MethodPost, "/api/tickets", bytes.NewReader(body)))
	if code, _ := errorBody(rec); rec.Code != http.StatusInternalServerError || code != "internal" {
		t.Errorf("with Redis down = %d %s, want 500 internal", rec.Code, rec.Body)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
