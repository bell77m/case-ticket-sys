package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// FR-A11: sign-in attempts count per username and per IP, in a fixed window that starts at the first attempt.
const (
	LoginWindow     = 15 * time.Minute
	maxUserFailures = 5
	maxIPFailures   = 20
)

// failKey hashes the value, so keys stay short whatever a client types.
func failKey(kind, v string) string {
	sum := sha256.Sum256([]byte(v))
	return "loginfail:" + kind + ":" + hex.EncodeToString(sum[:])
}

// LoginAttempt counts one attempt against username and ip before the password is checked, so parallel requests
// cannot slip past the limit together. It reports whether the attempt is over the limit (FR-A11). A right password
// takes the attempt back with LoginSucceeded; a wrong one keeps counting.
func (s *Sessions) LoginAttempt(ctx context.Context, username, ip string) (bool, error) {
	user, addr := failKey("user", username), failKey("ip", ip)
	pipe := s.Redis.TxPipeline()
	userCount := pipe.Incr(ctx, user)
	pipe.ExpireNX(ctx, user, LoginWindow)
	ipCount := pipe.Incr(ctx, addr)
	pipe.ExpireNX(ctx, addr, LoginWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("count login attempt: %w", err)
	}
	return userCount.Val() > maxUserFailures || ipCount.Val() > maxIPFailures, nil
}

// LoginSucceeded clears the username's failures and takes this attempt back from the IP's count.
func (s *Sessions) LoginSucceeded(ctx context.Context, username, ip string) error {
	addr := failKey("ip", ip)
	pipe := s.Redis.TxPipeline()
	pipe.Del(ctx, failKey("user", username))
	pipe.Decr(ctx, addr)
	pipe.ExpireNX(ctx, addr, LoginWindow) // the window may have ended mid-check; never leave a key without expiry
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("clear login attempts: %w", err)
	}
	return nil
}
