// Package events tells open staff pages that a ticket changed (FR-P3): Redis pub/sub carries the change to every
// pod, and each pod streams it to its browsers as server-sent events. An event holds only the ticket id; clients
// refetch through the authorized endpoints.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// Channel is the Redis pub/sub channel for ticket changes.
const Channel = "ticket-events"

// Heartbeat is how often a stream sends a ping and re-checks its session. Under NGINX's 1 h read timeout, and short
// enough that a deactivated staff member's stream ends soon (NFR-9). A variable so tests can shorten it.
var Heartbeat = 25 * time.Second

// Event is the payload of one ticket change.
type Event struct {
	Type string `json:"type"` // always "ticket"
	ID   int64  `json:"id"`
}

// Publish announces a change to ticket ticketID. Call it after the change commits, never inside the transaction.
func Publish(ctx context.Context, rdb *redis.Client, ticketID int64) error {
	b, _ := json.Marshal(Event{Type: "ticket", ID: ticketID})
	if err := rdb.Publish(ctx, Channel, b).Err(); err != nil {
		slog.Error("publish ticket event", "ticket_id", ticketID, "error", err)
		return fmt.Errorf("publish ticket event: %w", err)
	}
	return nil
}

// Serve streams ticket events to one client until it goes away or allowed returns false. allowed re-checks the
// session and permission at each heartbeat, since the stream outlives the request that authorized it (NFR-9).
//
// ponytail: one Redis connection per SSE client; switch to one subscription per pod fanned out to local clients
// if staff numbers grow into the hundreds.
func Serve(w http.ResponseWriter, r *http.Request, rdb *redis.Client, allowed func() bool) {
	ctx := r.Context()
	sub := rdb.Subscribe(ctx, Channel)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil { // subscribed before the 200, so no later change is missed
		slog.Error("subscribe ticket events", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"internal"}`)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // NGINX passes each event on at once
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if rc.Flush() != nil {
		return
	}

	msgs := sub.Channel()
	tick := time.NewTicker(Heartbeat)
	defer tick.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case m, ok := <-msgs:
			if !ok {
				return
			}
			_, err = fmt.Fprintf(w, "event: ticket\ndata: %s\n\n", m.Payload)
		case <-tick.C:
			if !allowed() {
				return
			}
			_, err = io.WriteString(w, ": ping\n\n")
		}
		if err != nil || rc.Flush() != nil {
			return
		}
	}
}
