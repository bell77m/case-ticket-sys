package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
)

// createTicketInput is everything a guest may send. Unknown fields are rejected, so a guest
// cannot set priority, category, status or assignee (FR-T3). No email is collected (FR-T4).
type createTicketInput struct {
	GuestName   string `json:"guest_name"`
	EmployeeID  string `json:"employee_id"`
	LocationID  int64  `json:"location_id"`
	CaseDetails string `json:"case_details"`
	Language    string `json:"language"`
}

type createTicketOutput struct {
	TicketID int64 `json:"ticket_id"`
	// Shown to the guest once; only its SHA-256 is stored (NFR-2). The frontend builds the link.
	TrackingToken string `json:"tracking_token"`
}

var languages = map[string]bool{"en": true, "zh-CN": true, "my": true, "th": true}

// validate trims fields in place and returns an error code per invalid field.
func (in *createTicketInput) validate() map[string]string {
	in.GuestName = strings.TrimSpace(in.GuestName)
	in.EmployeeID = strings.TrimSpace(in.EmployeeID)
	in.CaseDetails = strings.TrimSpace(strings.ReplaceAll(in.CaseDetails, crlf, lf))

	errs := map[string]string{}
	check := func(field, v string, lo, hi int) {
		if p := textProblem(field, v, lo, hi); p != "" {
			errs[field] = p
		}
	}
	check("guest_name", in.GuestName, 1, 100)
	check("employee_id", in.EmployeeID, 1, 20)
	check("case_details", in.CaseDetails, 10, 5000)
	if in.LocationID <= 0 {
		errs["location_id"] = "required"
	}
	if !languages[in.Language] {
		errs["language"] = "invalid"
	}
	return errs
}

// textProblem returns an error code for a guest text field, or "" when it is fine.
// field picks the character rules (see badRune); lo and hi are lengths in characters.
func textProblem(field, v string, lo, hi int) string {
	if strings.ContainsFunc(v, badRune(field)) {
		return "invalid_characters"
	}
	switch n := utf8.RuneCountInString(v); {
	case strings.TrimFunc(v, isZeroWidth) == "":
		return "required" // empty, or only invisible characters
	case n < lo:
		return "too_short"
	case n > hi:
		return "too_long"
	}
	return ""
}

const (
	crlf = string(rune(0x0D)) + string(rune(0x0A))
	lf   = string(rune(0x0A))
)

// isZeroWidth reports invisible spaces and joiners (U+200B-U+200D, U+FEFF). Burmese and Thai
// text uses them inside words, so they are allowed in names and details, but never alone.
func isZeroWidth(r rune) bool {
	return (r >= 0x200B && r <= 0x200D) || r == 0xFEFF
}

// isBidiControl reports characters that reorder displayed text (U+200E-F, U+202A-E, U+2066-9).
// A guest could use them to make a name or employee ID look like someone else's in the queue.
func isBidiControl(r rune) bool {
	return r == 0x200E || r == 0x200F || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// badRune returns the rejected-character test for a field.
func badRune(field string) func(rune) bool {
	switch field {
	case "employee_id": // staff compare IDs by eye; no invisible or format characters at all
		return func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }
	case "case_details": // multi-line text: line breaks and tabs allowed
		return func(r rune) bool { return (unicode.IsControl(r) && r != 0x0A && r != 0x09) || isBidiControl(r) }
	default:
		return func(r rune) bool { return unicode.IsControl(r) || isBidiControl(r) }
	}
}

// summarize returns the first 80 characters of the case details on one line (FR-T2).
func summarize(details string) string {
	s := strings.Join(strings.Fields(details), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80])
	}
	return s
}

// clientIP is the address the per-IP limits (FR-A11, NFR-3) and audit_log use: the TCP peer, or, when the peer is in
// TrustedProxies, the right-most X-Forwarded-For entry that is not a trusted proxy. Entries left of it are the
// client's own claims and are never read. A malformed entry or an all-trusted chain gives the peer.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	trusted := func(a netip.Addr) bool {
		return slices.ContainsFunc(s.TrustedProxies, func(p netip.Prefix) bool { return p.Contains(a.Unmap()) })
	}
	if peer, err := netip.ParseAddr(host); err != nil || !trusted(peer) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return host
		}
		if !trusted(a) {
			return a.Unmap().String()
		}
	}
	return host
}

var errLocationNotFound = errors.New("location not found")

// NFR-3: guest tickets per IP, in a fixed window that starts at the first request.
const (
	guestTicketWindow       = 10 * time.Minute
	defaultGuestTicketLimit = 5
)

// overGuestLimit counts one ticket request from ip and reports whether it is over the limit (NFR-3).
func (s *Server) overGuestLimit(ctx context.Context, ip string) (bool, error) {
	sum := sha256.Sum256([]byte(ip))
	key := "ratelimit:ticket:" + hex.EncodeToString(sum[:])
	pipe := s.Sessions.Redis.TxPipeline() // the sessions' Redis client, the only one the app has
	n := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, guestTicketWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("count guest ticket: %w", err)
	}
	limit := int64(s.GuestTicketLimit)
	if limit == 0 {
		limit = defaultGuestTicketLimit
	}
	return n.Val() > limit, nil
}

// POST /api/tickets — a guest opens a ticket (FR-G1).
func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	// Counted before the body is read, so invalid requests count too (NFR-3).
	switch over, err := s.overGuestLimit(r.Context(), s.clientIP(r)); {
	case err != nil:
		slog.Error("guest ticket limit", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	case over:
		writeError(w, http.StatusTooManyRequests, "ticket.rate_limited")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in createTicketInput
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if errs := in.validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": errs})
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("tracking token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	t := models.Ticket{
		Summary:         summarize(in.CaseDetails),
		CaseDetails:     in.CaseDetails,
		Status:          models.StatusNew,
		GuestName:       in.GuestName,
		EmployeeID:      in.EmployeeID,
		Language:        in.Language,
		LocationID:      in.LocationID,
		AccessTokenHash: hash[:],
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&models.Location{}).Where("id = ? AND is_active", in.LocationID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return errLocationNotFound
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Guest, "ticket.created", audit.Change{
			TicketID: &t.ID,
			Target:   in.EmployeeID,
			To:       "location:" + strconv.FormatInt(in.LocationID, 10),
			IP:       s.clientIP(r),
		})
	})
	switch {
	case errors.Is(err, errLocationNotFound):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"location_id": "not_found"}})
	case err != nil:
		slog.Error("create ticket", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	default:
		s.publish(r.Context(), t.ID) // FR-P3
		writeJSON(w, http.StatusCreated, createTicketOutput{TicketID: t.ID, TrackingToken: token})
	}
}
