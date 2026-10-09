package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"ticket-app/internal/auth"
	"ticket-app/internal/events"
	"ticket-app/internal/rbac"
)

// server is one more app instance (a pod) on the env's DB and Redis, with a Redis client of its own.
func (e *testEnv) server() *httptest.Server {
	e.t.Helper()
	rdb := redis.NewClient(e.sessions.Redis.Options())
	mux := http.NewServeMux()
	(&Server{DB: e.db, UploadDir: e.dir, Sessions: &auth.Sessions{Redis: rdb}, GuestTicketLimit: 1000, ClamdAddr: e.clamd}).Routes(mux)
	srv := httptest.NewServer(mux)
	e.t.Cleanup(func() {
		srv.Close()
		_ = rdb.Close()
	})
	return srv
}

// stream opens GET /api/events on srv with session c and returns its lines; the channel closes when the stream ends.
func (e *testEnv) stream(srv *httptest.Server, c *http.Cookie) <-chan string {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/events", nil)
	req.AddCookie(c)
	resp, err := srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = resp.Body.Close() }) // before srv.Close, which waits for open streams
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("GET /api/events = %d, want 200", resp.StatusCode)
	}
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return lines
}

// waitLine reads lines until want, failing if the stream ends or within does not pass first. Other tests on this
// Redis publish too, so other lines are skipped.
func waitLine(t *testing.T, lines <-chan string, want string, within time.Duration) {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream ended before %q", want)
			}
			if line == want {
				return
			}
		case <-timeout:
			t.Fatalf("no %q within %v", want, within)
		}
	}
}

func eventLine(id int64) string { return fmt.Sprintf(`data: {"type":"ticket","id":%d}`, id) }

// FR-P3: with two app instances on one Redis, a status change made through instance A reaches a staff member's
// stream on instance B within 2 seconds.
func TestEventsAcrossInstances_FRP3(t *testing.T) {
	e := newAuthEnv(t)
	a, b := e.server(), e.server()
	c := e.session(e.newStaff("Agent", true))
	tk := e.newTicket()
	lines := e.stream(b, c)

	start := time.Now()
	req, _ := http.NewRequest(http.MethodPatch, a.URL+ticketPath(tk.TicketID), strings.NewReader(`{"status":"in_progress"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(c)
	resp, err := a.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PATCH on A = %d, want 204", resp.StatusCode)
	}
	waitLine(t, lines, eventLine(tk.TicketID), 2*time.Second)
	t.Logf("event reached instance B %v after the PATCH on instance A began", time.Since(start))
}

// NFR-9, FR-A10, FR-R3: a stream outlives the request that authorized it, so each heartbeat re-checks the session
// and the permission; an open stream ends at the next heartbeat once either fails.
func TestEventsRecheck_NFR9(t *testing.T) {
	old := events.Heartbeat
	events.Heartbeat = 50 * time.Millisecond
	t.Cleanup(func() { events.Heartbeat = old }) // registered first, so it runs after the servers close
	e := newAuthEnv(t)
	srv := e.server()

	tests := []struct {
		name   string
		change string // SQL; its one argument is the staff ID, or the role ID when byRole
		byRole bool
	}{
		{"deactivated", "UPDATE staff SET is_active = false WHERE id = ?", false},
		{"password reset", "UPDATE staff SET password_changed_at = now() WHERE id = ?", false},
		{"permission removed", "DELETE FROM role_permissions WHERE role_id = ?", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roleID, role := e.newRole()
			if err := e.db.Exec("INSERT INTO role_permissions VALUES (?, ?)", roleID, rbac.TicketViewAll).Error; err != nil {
				t.Fatal(err)
			}
			st := e.newStaff(role, true)
			lines := e.stream(srv, e.session(st))
			waitLine(t, lines, ": ping", time.Second) // still allowed: the stream stays open

			arg := st.ID
			if tt.byRole {
				arg = roleID
			}
			if err := e.db.Exec(tt.change, arg).Error; err != nil {
				t.Fatal(err)
			}
			timeout := time.After(time.Second)
			for {
				select {
				case _, ok := <-lines:
					if !ok {
						return // stream ended
					}
				case <-timeout:
					t.Fatal("stream still open 1s after the change")
				}
			}
		})
	}
}

// ticketEvents subscribes to ticket events in Redis and passes on the ticket ids.
func (e *testEnv) ticketEvents() <-chan int64 {
	e.t.Helper()
	sub := e.sessions.Redis.Subscribe(context.Background(), events.Channel)
	if _, err := sub.Receive(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = sub.Close() })
	ids := make(chan int64, 64)
	go func() {
		for m := range sub.Channel() {
			var ev events.Event
			if json.Unmarshal([]byte(m.Payload), &ev) == nil {
				ids <- ev.ID
			}
		}
	}()
	return ids
}

// FR-P3: guest actions publish the ticket id too, so staff queues update when a guest writes.
func TestGuestPublish_FRP3(t *testing.T) {
	e := newTestEnv(t)
	ids := e.ticketEvents()
	photo := append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{1}, 1000)...)
	var tk createTicketOutput
	steps := []struct {
		name string
		do   func() int // returns the response status
	}{
		{"guest create", func() int { tk = e.newTicket(); return http.StatusCreated }},
		{"attachment upload", func() int { return e.upload(tk.TicketID, tk.TrackingToken, "p.jpg", photo).Code }},
		{"guest reply", func() int { return e.reply(tk.TrackingToken, "Still broken").code }},
	}
	for _, s := range steps {
		if code := s.do(); code != http.StatusCreated {
			t.Fatalf("%s = %d, want 201", s.name, code)
		}
		timeout := time.After(2 * time.Second)
	wait:
		for {
			select {
			case id := <-ids:
				if id == tk.TicketID {
					break wait
				}
			case <-timeout:
				t.Fatalf("%s: no event for ticket %d within 2s", s.name, tk.TicketID)
			}
		}
	}
}
