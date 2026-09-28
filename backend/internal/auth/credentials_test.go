package auth

import (
	"strings"
	"testing"
	"time"
)

// FR-A7: a hash verifies its own password only, two hashes of one password differ (random salt), and the
// format names its algorithm and work factor so both can change later.
func TestHashPassword_FRA7(t *testing.T) {
	for _, pw := range []string{"correct horse battery", "รหัสผ่านภาษาไทยยาว", "မြန်မာစကားဝှက်ရှည်"} {
		h1, err := HashPassword(pw)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(h1, "pbkdf2-sha256$600000$") || strings.Count(h1, "$") != 3 {
			t.Errorf("hash %q, want pbkdf2-sha256$600000$<salt>$<key>", h1)
		}
		h2, _ := HashPassword(pw)
		if h1 == h2 {
			t.Errorf("two hashes of %q are equal; salt is not random", pw)
		}
		if !CheckPassword(h1, pw) {
			t.Errorf("CheckPassword(own hash, %q) = false", pw)
		}
		if CheckPassword(h1, pw+"x") || CheckPassword(h1, strings.ToUpper(pw)+"X") {
			t.Errorf("CheckPassword accepted a different password for %q", pw)
		}
	}
}

// FR-A2: an account without a password (empty hash) or with a broken hash never matches.
func TestCheckPassword_NoHash_FRA2(t *testing.T) {
	for _, enc := range []string{"", "plain", "pbkdf2-sha256$x$y$z", "bcrypt$1$2$3", "pbkdf2-sha256$600000$!!$!!"} {
		if CheckPassword(enc, "") || CheckPassword(enc, "anything at all") {
			t.Errorf("CheckPassword(%q, ...) = true, want false", enc)
		}
	}
}

// FR-A2: checking against no hash costs about as much as a real check, so response time does not tell
// which usernames exist.
func TestCheckPassword_SameCost_FRA2(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	CheckPassword("", "warm up the dummy hash")
	start := time.Now()
	CheckPassword(h, "wrong password!")
	real := time.Since(start)
	start = time.Now()
	CheckPassword("", "wrong password!")
	none := time.Since(start)
	if none < real/2 {
		t.Errorf("check without a hash took %v, real check %v; want about the same", none, real)
	}
}

// FR-A7: 12 to 128 characters, counted as characters (Thai and Burmese letters are several bytes each).
func TestPasswordProblem_FRA7(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "required"},
		{strings.Repeat("a", 11), "too_short"},
		{strings.Repeat("a", 12), ""},
		{strings.Repeat("ก", 12), ""},
		{strings.Repeat("a", 128), ""},
		{strings.Repeat("a", 129), "too_long"},
		{"            ", ""}, // spaces are characters too; no composition rules (NIST 800-63B)
	}
	for _, tt := range tests {
		if got := PasswordProblem(tt.in); got != tt.want {
			t.Errorf("PasswordProblem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// FR-A1: usernames are 3 to 64 characters from a-z 0-9 . _ - @, compared in lower case.
func TestUsernameProblem_FRA1(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "required"},
		{"ab", "invalid"},
		{"abc", ""},
		{"somchai.k", ""},
		{"a_b-c@d.e", ""},
		{strings.Repeat("a", 64), ""},
		{strings.Repeat("a", 65), "invalid"},
		{"Somchai", "invalid"}, // callers normalize first
		{"som chai", "invalid"},
		{"som+chai", "invalid"},
		{"ซมชาย", "invalid"},
	}
	for _, tt := range tests {
		if got := UsernameProblem(tt.in); got != tt.want {
			t.Errorf("UsernameProblem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := NormalizeUsername("  SomChai.K "); got != "somchai.k" {
		t.Errorf("NormalizeUsername = %q, want somchai.k", got)
	}
}
