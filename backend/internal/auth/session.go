// Package auth checks staff passwords (FR-A7) and keeps sessions and failed sign-in counts in Redis.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionTTL is how long a sign-in lasts; after it the staff member signs in again.
const SessionTTL = 8 * time.Hour

// ErrNotFound means the session does not exist or has expired.
var ErrNotFound = errors.New("auth: not found or expired")

// Sessions stores staff sessions and failed sign-in counts in Redis.
type Sessions struct {
	Redis *redis.Client
}

// Keys hold a SHA-256 of the session ID, so a Redis dump does not contain usable cookies.
func sessionKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "session:" + hex.EncodeToString(sum[:])
}

// Session is a signed-in staff member. Stamp is their password_changed_at in Unix microseconds when the session
// began: after a password change or reset the row's stamp differs and the API refuses the session (FR-A10).
type Session struct {
	StaffID int64
	Stamp   int64
}

// Create starts a session and returns its ID for the cookie (128 random bits).
func (s *Sessions) Create(ctx context.Context, staffID, stamp int64) (string, error) {
	id := rand.Text()
	v := strconv.FormatInt(staffID, 10) + ":" + strconv.FormatInt(stamp, 10)
	if err := s.Redis.Set(ctx, sessionKey(id), v, SessionTTL).Err(); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// Get returns a live session, or ErrNotFound. A value in an older format counts as not found.
func (s *Sessions) Get(ctx context.Context, id string) (Session, error) {
	v, err := s.Redis.Get(ctx, sessionKey(id)).Result()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	staff, stamp, ok := strings.Cut(v, ":")
	staffID, err1 := strconv.ParseInt(staff, 10, 64)
	st, err2 := strconv.ParseInt(stamp, 10, 64)
	if !ok || err1 != nil || err2 != nil {
		return Session{}, ErrNotFound
	}
	return Session{StaffID: staffID, Stamp: st}, nil
}

// Delete ends a session. Deleting a missing session is not an error.
func (s *Sessions) Delete(ctx context.Context, id string) error {
	if err := s.Redis.Del(ctx, sessionKey(id)).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
