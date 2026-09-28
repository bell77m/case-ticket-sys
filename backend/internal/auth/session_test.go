package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// FR-A10: a session carries the password stamp it began with. A session saved before T2.15 holds only a staff
// ID; it reads as not found, so that browser signs in again instead of failing.
func TestSessionOldFormat_FRA10(t *testing.T) {
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
	ctx, s := context.Background(), &Sessions{Redis: rdb}

	old := rand.Text()
	if err := rdb.Set(ctx, sessionKey(old), 42, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Delete(ctx, old) })
	if _, err := s.Get(ctx, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old-format session: err = %v, want ErrNotFound", err)
	}

	id, err := s.Create(ctx, 42, 1700000000123456)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Delete(ctx, id) })
	if got, err := s.Get(ctx, id); err != nil || got != (Session{StaffID: 42, Stamp: 1700000000123456}) {
		t.Errorf("Get = %+v, %v; want staff 42 with its stamp", got, err)
	}
}
