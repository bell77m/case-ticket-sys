package autoclose

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticket-app/internal/events"
	"ticket-app/internal/models"
)

// now is the fake clock. It lies far in the past, so Run never touches the dev DB's real tickets
// (resolved_at is set from the DB clock) or other tests' tickets.
var now = time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC)

type env struct {
	t   *testing.T
	db  *gorm.DB
	rdb *redis.Client
	ctx context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dbURL, redisURL := os.Getenv("DATABASE_URL"), os.Getenv("REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("DATABASE_URL or REDIS_URL not set")
	}
	db, err := models.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, rdb: redis.NewClient(opt), ctx: context.Background()}
	t.Cleanup(func() {
		e.rdb.Del(e.ctx, lockKey)
		_ = e.rdb.Close()
	})
	return e
}

// ticket inserts a fixture ticket at a seeded location and deletes it after the test.
func (e *env) ticket(status string, resolvedAt *time.Time) int64 {
	e.t.Helper()
	var loc models.Location
	if err := e.db.Where("is_active").First(&loc).Error; err != nil {
		e.t.Fatalf("need a seeded location (make seed): %v", err)
	}
	hash := make([]byte, 32)
	_, _ = rand.Read(hash)
	tk := models.Ticket{Summary: "auto-close fixture", CaseDetails: "auto-close fixture details", Status: status,
		GuestName: "Fixture", EmployeeID: "E0", Language: "en", LocationID: loc.ID, AccessTokenHash: hash, ResolvedAt: resolvedAt}
	if err := e.db.Create(&tk).Error; err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { e.db.Exec("DELETE FROM tickets WHERE id = ?", tk.ID) })
	return tk.ID
}

func (e *env) status(id int64) string {
	e.t.Helper()
	var s string
	if err := e.db.Raw("SELECT status FROM tickets WHERE id = ?", id).Scan(&s).Error; err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) auditRows(id int64) []models.AuditEntry {
	e.t.Helper()
	var rows []models.AuditEntry
	if err := e.db.Where("ticket_id = ?", id).Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func ago(d time.Duration) *time.Time {
	t := now.Add(-d)
	return &t
}

// FR-T5, FR-L1: a ticket Resolved for 7 days closes once, by actor "system", with one audit row.
func TestAutoClose_FRT5(t *testing.T) {
	e := newEnv(t)
	day := 24 * time.Hour
	fixtures := []struct {
		name       string
		status     string
		resolvedAt *time.Time
		wantClose  bool
		id         int64
	}{
		{"resolved 8 days ago", models.StatusResolved, ago(8 * day), true, 0},
		{"resolved exactly 7 days ago", models.StatusResolved, ago(7 * day), true, 0},
		{"resolved 6 days 23 hours ago", models.StatusResolved, ago(7*day - time.Hour), false, 0},
		{"in progress with old resolved_at", models.StatusInProgress, ago(30 * day), false, 0},
		{"closed", models.StatusClosed, ago(30 * day), false, 0},
	}
	for i := range fixtures {
		fixtures[i].id = e.ticket(fixtures[i].status, fixtures[i].resolvedAt)
	}

	// FR-P3: each closed ticket is published, so open staff queues refresh.
	sub := e.rdb.Subscribe(e.ctx, events.Channel)
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(e.ctx); err != nil {
		t.Fatal(err)
	}

	e.rdb.Del(e.ctx, lockKey)
	n, err := Run(e.ctx, e.db, e.rdb, now)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	published := map[int64]bool{}
	for timeout := time.After(2 * time.Second); !published[fixtures[0].id] || !published[fixtures[1].id]; {
		select {
		case m := <-sub.Channel():
			var ev events.Event
			if json.Unmarshal([]byte(m.Payload), &ev) == nil {
				published[ev.ID] = true
			}
		case <-timeout:
			t.Fatalf("closed tickets %d and %d not both published within 2s: %v", fixtures[0].id, fixtures[1].id, published)
		}
	}
	for _, f := range fixtures[2:] {
		if published[f.id] {
			t.Errorf("ticket %d published but not closed", f.id)
		}
	}
	if n < 2 { // counts every eligible ticket in the DB, not only the fixtures
		t.Errorf("first Run closed %d, want at least 2", n)
	}
	if ttl := e.rdb.TTL(e.ctx, lockKey).Val(); ttl <= 54*time.Minute || ttl > 55*time.Minute {
		t.Errorf("lock TTL = %v, want 55m", ttl)
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			want, wantAudit := f.status, 0
			if f.wantClose {
				want, wantAudit = models.StatusClosed, 1
			}
			if got := e.status(f.id); got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
			rows := e.auditRows(f.id)
			if len(rows) != wantAudit {
				t.Fatalf("audit rows = %d, want %d", len(rows), wantAudit)
			}
			if wantAudit == 1 {
				r := rows[0]
				if r.Action != "ticket.auto_closed" || r.ActorType != models.ActorSystem || r.ActorStaffID != nil ||
					r.FromValue == nil || *r.FromValue != "resolved" || r.ToValue == nil || *r.ToValue != "closed" {
					t.Errorf("audit row = %+v", r)
				}
			}
		})
	}

	// Next hour: nothing left to close, and no second audit row.
	e.rdb.Del(e.ctx, lockKey)
	if n, err := Run(e.ctx, e.db, e.rdb, now); n != 0 || err != nil {
		t.Errorf("second Run = %d, %v; want 0, nil", n, err)
	}
	for _, f := range fixtures {
		if f.wantClose && len(e.auditRows(f.id)) != 1 {
			t.Errorf("%s: audit rows after second Run = %d, want 1", f.name, len(e.auditRows(f.id)))
		}
	}
}

// FR-T5: when another pod holds the lock this hour, Run does nothing.
func TestAutoCloseLockHeld_FRT5(t *testing.T) {
	e := newEnv(t)
	id := e.ticket(models.StatusResolved, ago(8*24*time.Hour))
	e.rdb.Del(e.ctx, lockKey)
	if err := e.rdb.Set(e.ctx, lockKey, "other-pod", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	n, err := Run(e.ctx, e.db, e.rdb, now)
	if n != 0 || err != nil {
		t.Errorf("Run = %d, %v; want 0, nil", n, err)
	}
	if got := e.status(id); got != models.StatusResolved {
		t.Errorf("status = %q, want resolved", got)
	}
	if rows := e.auditRows(id); len(rows) != 0 {
		t.Errorf("audit rows = %d, want 0", len(rows))
	}
}
