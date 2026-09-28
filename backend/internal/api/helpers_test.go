package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticket-app/internal/auth"
	"ticket-app/internal/models"
)

// testEnv is a Server on the dev DB (migrated and seeded) and dev Redis, with a temp upload dir.
type testEnv struct {
	t   *testing.T
	db  *gorm.DB
	mux *http.ServeMux
	loc models.Location
	dir string

	sessions *auth.Sessions
	ip       string // client IP for sign-ins; set by newAuthEnv
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := models.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set") // sessions and the guest ticket limit (NFR-3) live in Redis
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	e := &testEnv{t: t, db: db, mux: http.NewServeMux(), dir: t.TempDir(), sessions: &auth.Sessions{Redis: rdb}}
	if err := db.Where("is_active").First(&e.loc).Error; err != nil {
		t.Fatalf("need a seeded location (make seed): %v", err)
	}
	// Tests open many tickets from httptest's one client IP; TestGuestRateLimit_NFR3 covers the real limit.
	(&Server{DB: db, UploadDir: e.dir, Sessions: e.sessions, GuestTicketLimit: 1000}).Routes(e.mux)
	return e
}

// do sends a request with an optional tracking token and returns the recorder.
func (e *testEnv) do(method, path, token string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("X-Tracking-Token", token)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// newTicket creates a guest ticket through the API and deletes it after the test. edit changes the input first.
func (e *testEnv) newTicket(edit ...func(*createTicketInput)) createTicketOutput {
	e.t.Helper()
	in := validInput()
	in.LocationID = e.loc.ID
	for _, f := range edit {
		f(&in)
	}
	body, _ := json.Marshal(in)
	rec := e.do(http.MethodPost, "/api/tickets", "", bytes.NewReader(body), "application/json")
	var out createTicketOutput
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TicketID == 0 {
		e.t.Fatalf("create ticket: %d %s", rec.Code, rec.Body)
	}
	e.t.Cleanup(func() { e.db.Exec("DELETE FROM tickets WHERE id = ?", out.TicketID) })
	return out
}

// upload posts one file as multipart form field "file".
func (e *testEnv) upload(ticketID int64, token, filename string, data []byte) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	return e.do(http.MethodPost, "/api/tickets/"+strconv.FormatInt(ticketID, 10)+"/attachments", token, &buf, mw.FormDataContentType())
}

// setStatus changes a ticket's status directly, standing in for staff actions not built yet.
func (e *testEnv) setStatus(id int64, status string) {
	e.t.Helper()
	if err := e.db.Exec("UPDATE tickets SET status = ? WHERE id = ?", status, id).Error; err != nil {
		e.t.Fatal(err)
	}
}
