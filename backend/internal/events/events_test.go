package events

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// shortHeartbeat sets Heartbeat to d until the test ends. Call it before starting servers, so it is restored after
// they close.
func shortHeartbeat(t *testing.T, d time.Duration) {
	old := Heartbeat
	Heartbeat = d
	t.Cleanup(func() { Heartbeat = old })
}

// next returns the next line from lines, or fails the test after timeout. ok is false once the stream ended.
func next(t *testing.T, lines <-chan string, timeout time.Duration) (line string, ok bool) {
	t.Helper()
	select {
	case line, ok = <-lines:
		return line, ok
	case <-time.After(timeout):
		t.Fatalf("no line within %v", timeout)
		return "", false
	}
}

// FR-P3, NFR-9: the stream sends the SSE headers, pings at each heartbeat, outlives the server's ReadTimeout,
// forwards published ticket events, and ends at the first heartbeat whose re-check fails.
func TestEvents_FRP3(t *testing.T) {
	rdb := redisClient(t)
	shortHeartbeat(t, 50*time.Millisecond)
	var allowed atomic.Bool
	allowed.Store(true)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Serve(w, r, rdb, allowed.Load)
	}))
	srv.Config.ReadTimeout = 200 * time.Millisecond // like main.go's ReadTimeout, only shorter
	srv.Start()
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for _, h := range []struct{ name, want string }{
		{"Content-Type", "text/event-stream"},
		{"Cache-Control", "no-store"},
		{"X-Accel-Buffering", "no"}, // NGINX must not buffer the stream
	} {
		if got := resp.Header.Get(h.name); got != h.want {
			t.Errorf("%s = %q, want %q", h.name, got, h.want)
		}
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	// Pings keep coming past the ReadTimeout: the stream is not cut off with the request's read deadline.
	// Other test packages publish on the same Redis meanwhile, so lines other than pings may show up too.
	pings := 0
	for start := time.Now(); time.Since(start) < 400*time.Millisecond; {
		line, ok := next(t, lines, time.Second)
		if !ok {
			t.Fatalf("stream ended after %v", time.Since(start))
		}
		if line == ": ping" {
			pings++
		}
	}
	if pings < 4 {
		t.Errorf("pings in 400ms = %d, want at least 4 at a 50ms heartbeat", pings)
	}

	id := time.Now().UnixNano() // unique, so events from other tests on this Redis do not match
	if err := Publish(context.Background(), rdb, id); err != nil {
		t.Fatal(err)
	}
	want := `data: {"type":"ticket","id":` + strconv.FormatInt(id, 10) + `}`
	for prev := ""; ; {
		line, ok := next(t, lines, time.Second)
		if !ok {
			t.Fatal("stream ended before the event")
		}
		if line == want {
			if prev != "event: ticket" {
				t.Errorf("line before the data = %q, want %q", prev, "event: ticket")
			}
			break
		}
		prev = line
	}

	allowed.Store(false) // the session ended (NFR-9)
	for {
		if _, ok := next(t, lines, time.Second); !ok {
			break
		}
	}
}

// FR-P3: without Redis the handler answers 500 with an error code, not a stream.
func TestEventsRedisDown_FRP3(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	rec := httptest.NewRecorder()
	Serve(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil), rdb, func() bool { return true })
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"error":"internal"}` {
		t.Errorf("got %d %s, want 500 internal", rec.Code, rec.Body)
	}
	if err := Publish(context.Background(), rdb, 1); err == nil {
		t.Error("Publish without Redis: want an error")
	}
}
