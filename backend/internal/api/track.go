package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
)

// Guest tracking endpoints (FR-G3–G5). The guest proves the ticket is theirs with the tracking
// token in the X-Tracking-Token header. The page link carries it after "#", which browsers never
// send to a server, so it stays out of proxy logs and Referer headers.

// trackView is everything a token holder may see. No guest name, employee ID, staff names or internal notes:
// anyone the link is shared with sees this (FR-G4, FR-G5).
type trackView struct {
	TicketID    int64             `json:"ticket_id"`
	Status      string            `json:"status"`
	CaseDetails string            `json:"case_details"`
	CreatedAt   time.Time         `json:"created_at"`
	Location    trackLocation     `json:"location"`
	Comments    []trackComment    `json:"comments"`
	Attachments []trackAttachment `json:"attachments"`
}

type trackLocation struct {
	Building string `json:"building"`
	Floor    string `json:"floor"`
	Line     string `json:"line"`
}

type trackComment struct {
	ID        int64     `json:"id"`
	From      string    `json:"from"` // "guest" or "staff"
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type trackAttachment struct {
	ID        int64     `json:"id"`
	MediaType string    `json:"media_type"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// guestTicket finds the ticket for the request's tracking token, or writes a 404.
// A wrong token and an unknown ticket look the same, so ticket IDs cannot be probed.
func (s *Server) guestTicket(w http.ResponseWriter, r *http.Request) (models.Ticket, bool) {
	var t models.Ticket
	token := r.Header.Get("X-Tracking-Token")
	if token != "" {
		hash := sha256.Sum256([]byte(token))
		if err := s.DB.WithContext(r.Context()).Where("access_token_hash = ?", hash[:]).First(&t).Error; err == nil {
			return t, true
		}
	}
	writeError(w, http.StatusNotFound, "ticket.not_found")
	return t, false
}

// GET /api/track?lang=th
func (s *Server) trackView(w http.ResponseWriter, r *http.Request) {
	t, ok := s.guestTicket(w, r)
	if !ok {
		return
	}
	db := s.DB.WithContext(r.Context())
	lang := r.URL.Query().Get("lang")
	v := trackView{TicketID: t.ID, Status: t.Status, CaseDetails: t.CaseDetails, CreatedAt: t.CreatedAt,
		Comments: []trackComment{}, Attachments: []trackAttachment{}}

	var loc models.Location
	var comments []models.Comment
	var files []models.Attachment
	err := errors.Join(
		db.First(&loc, t.LocationID).Error,
		db.Where("ticket_id = ? AND NOT is_internal", t.ID).Order("created_at, id").Find(&comments).Error,
		db.Where("ticket_id = ?", t.ID).Order("id").Find(&files).Error,
	)
	if err != nil {
		slog.Error("track view", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	v.Location = trackLocation{pickName(loc.Building, lang), pickName(loc.Floor, lang), pickName(loc.Line, lang)}
	for _, c := range comments {
		from := "staff"
		if c.AuthorStaffID == nil {
			from = "guest"
		}
		v.Comments = append(v.Comments, trackComment{ID: c.ID, From: from, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, f := range files {
		v.Attachments = append(v.Attachments, trackAttachment{ID: f.ID, MediaType: f.MediaType, SizeBytes: f.SizeBytes, CreatedAt: f.CreatedAt})
	}
	writeJSON(w, http.StatusOK, v)
}

var errNotResolved = errors.New("ticket not resolved")

// withLockedTicket runs fn in a transaction holding a row lock on the ticket, so a staff
// status change cannot interleave with the guest's action. Once fn's work commits, it publishes the ticket's id:
// every staff and guest ticket action goes through here.
func (s *Server) withLockedTicket(r *http.Request, id int64, fn func(tx *gorm.DB, t *models.Ticket) error) error {
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var t models.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error; err != nil {
			return err
		}
		return fn(tx, &t)
	})
	if err == nil {
		s.publish(r.Context(), id) // after the commit, so pages that refetch see the change (FR-P3)
	}
	return err
}

// setStatus changes a ticket's status and audits it, inside tx.
func setStatus(tx *gorm.DB, t *models.Ticket, to string, actor audit.Actor, ip string) error {
	from := t.Status // read first: GORM's Updates writes the new values into t
	updates := map[string]any{"status": to}
	switch to {
	case models.StatusResolved:
		updates["resolved_at"] = gorm.Expr("now()") // DB clock, like created_at; auto-close counts from it (FR-T5)
	case models.StatusClosed:
		updates["resolved_at"] = gorm.Expr("coalesce(resolved_at, now())") // closed straight from open counts as resolved now
	default:
		updates["resolved_at"] = nil // reopened: resolution time restarts
	}
	if err := tx.Model(t).Updates(updates).Error; err != nil {
		return err
	}
	return audit.Record(tx, actor, "ticket.status_changed", audit.Change{TicketID: &t.ID, From: from, To: to, IP: ip})
}

// POST /api/track/comments {"body": "..."} — the guest replies (FR-G3). Replying to a ticket that is
// waiting on the guest, or already resolved, moves it back to In Progress (the lifecycle's reopen).
func (s *Server) trackReply(w http.ResponseWriter, r *http.Request) {
	t, ok := s.guestTicket(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in struct {
		Body string `json:"body"`
	}
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	body := strings.TrimSpace(strings.ReplaceAll(in.Body, crlf, lf))
	if p := textProblem("case_details", body, 1, 5000); p != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"body": p}})
		return
	}

	var c models.Comment
	err := s.withLockedTicket(r, t.ID, func(tx *gorm.DB, t *models.Ticket) error {
		if t.Status == models.StatusClosed {
			return errTicketClosed
		}
		c = models.Comment{TicketID: t.ID, Body: body} // no author = guest; never internal
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		if err := audit.Record(tx, audit.Guest, "comment.added", audit.Change{TicketID: &t.ID, IP: s.clientIP(r)}); err != nil {
			return err
		}
		if t.Status == models.StatusWaiting || t.Status == models.StatusResolved {
			return setStatus(tx, t, models.StatusInProgress, audit.Guest, s.clientIP(r))
		}
		return nil
	})
	switch {
	case errors.Is(err, errTicketClosed):
		writeError(w, http.StatusConflict, "ticket.closed")
	case err != nil:
		slog.Error("guest reply", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	default:
		writeJSON(w, http.StatusCreated, trackComment{ID: c.ID, From: "guest", Body: c.Body, CreatedAt: c.CreatedAt})
	}
}

// POST /api/track/confirm — the guest confirms the fix; only a resolved ticket closes (FR-G3, FR-T5).
func (s *Server) trackConfirm(w http.ResponseWriter, r *http.Request) {
	t, ok := s.guestTicket(w, r)
	if !ok {
		return
	}
	err := s.withLockedTicket(r, t.ID, func(tx *gorm.DB, t *models.Ticket) error {
		if t.Status != models.StatusResolved {
			return errNotResolved
		}
		return setStatus(tx, t, models.StatusClosed, audit.Guest, s.clientIP(r))
	})
	switch {
	case errors.Is(err, errNotResolved):
		writeError(w, http.StatusConflict, "ticket.not_resolved")
	case err != nil:
		slog.Error("guest confirm", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": models.StatusClosed})
	}
}

// GET /api/track/attachments/{id} — one evidence file of the token's own ticket.
// The Content-Type comes from the upload allowlist, never from the client or a generic sniff, and
// nosniff plus a sandbox CSP stop a file with a valid image header from running as a page (NFR-5).
func (s *Server) trackFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.guestTicket(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var a models.Attachment
	if err != nil || s.DB.WithContext(r.Context()).Where("id = ? AND ticket_id = ?", id, t.ID).First(&a).Error != nil {
		writeError(w, http.StatusNotFound, "file.not_found")
		return
	}
	serveEvidence(w, r, filepath.Join(s.UploadDir, filepath.FromSlash(a.FilePath)))
}

// serveEvidence streams a stored file with safe headers. Range requests work, so videos can seek.
func serveEvidence(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		slog.Error("open evidence", "error", err)
		writeError(w, http.StatusNotFound, "file.not_found")
		return
	}
	defer func() { _ = f.Close() }() // read-only file: a close error loses nothing
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	_, mime := sniffMedia(head[:n])
	if mime == "" {
		mime = "application/octet-stream"
	}
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
