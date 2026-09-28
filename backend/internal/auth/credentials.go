package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// passwordIter is the PBKDF2-HMAC-SHA256 work factor (OWASP 2023). Each hash stores its own count, so it can
// rise later without breaking existing passwords.
const passwordIter = 600_000

// FR-A7: length limits in characters; no composition rules (NIST 800-63B).
const (
	passwordMin = 12
	passwordMax = 128
)

var b64 = base64.RawStdEncoding

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<key>" with a 16-byte random salt (FR-A7).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt) // never fails: crypto/rand crashes the program instead (Go 1.24+)
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIter, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// dummyHash stands in for an unknown account or one without a password, so checking it costs the same.
var dummyHash = sync.OnceValue(func() string {
	h, err := HashPassword(rand.Text())
	if err != nil {
		panic(err)
	}
	return h
})

// CheckPassword reports whether password matches encoded. An empty or foreign encoded value never matches but
// still costs one full hash, so response time does not tell which usernames exist (FR-A2).
func CheckPassword(encoded, password string) bool {
	match := true
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		match = false
		parts = strings.Split(dummyHash(), "$")
	}
	iter, err1 := strconv.Atoi(parts[1])
	salt, err2 := b64.DecodeString(parts[2])
	want, err3 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || iter < 1 || len(want) == 0 {
		return false // a corrupt row; only HashPassword writes this column
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	return err == nil && match && subtle.ConstantTimeCompare(got, want) == 1
}

// PasswordProblem returns "required", "too_short" or "too_long" (FR-A7), or "" when the password is fine.
func PasswordProblem(p string) string {
	switch n := utf8.RuneCountInString(p); {
	case n == 0:
		return "required"
	case n < passwordMin:
		return "too_short"
	case n > passwordMax:
		return "too_long"
	}
	return ""
}

var usernameRE = regexp.MustCompile(`^[a-z0-9._@-]{3,64}$`)

// NormalizeUsername is the form usernames are stored and compared in: trimmed, lower case.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// UsernameProblem returns "required" or "invalid" for a normalized username, or "" when it is fine.
func UsernameProblem(u string) string {
	switch {
	case u == "":
		return "required"
	case !usernameRE.MatchString(u):
		return "invalid"
	}
	return ""
}
