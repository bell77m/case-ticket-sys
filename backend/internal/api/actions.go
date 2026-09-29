package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
)

// Staff ticket actions (T2.04): status, priority, category (FR-T3, FR-T5) and assignee; comments (T2.05, FR-T6).

// staffTransitions are the lifecycle moves staff may make (docs/REQUIREMENTS.md). Nothing goes to or from
// closed: a ticket closes only when the guest confirms or the auto-close job runs (FR-T5).
var staffTransitions = map[[2]string]bool{
	{models.StatusNew, models.StatusInProgress}:      true,
	{models.StatusInProgress, models.StatusWaiting}:  true,
	{models.StatusInProgress, models.StatusResolved}: true,
	{models.StatusWaiting, models.StatusInProgress}:  true,
	{models.StatusResolved, models.StatusInProgress}: true,
}

var errBadTransition = errors.New("status transition not allowed")

type ticketPatch struct {
	Status     *string `json:"status"`
	Priority   *string `json:"priority"`
	CategoryID *int64  `json:"category_id"`
}

// PATCH /api/staff/tickets/{id} {"status"?, "priority"?, "category_id"?} — one audit row per changed field.
func (s *Server) updateTicket(w http.ResponseWriter, r *http.Request) {
	t, ok := s.staffTicketByPath(w, r)
	if !ok {
		return
	}
	var in ticketPatch
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Status == nil && in.Priority == nil && in.CategoryID == nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	fields := map[string]string{}
	if in.Status != nil && !queueStatuses[*in.Status] {
		fields["status"] = "invalid"
	}
	if in.Priority != nil && (!queuePriorities[*in.Priority] || *in.Priority == "none") { // "none" is a queue filter only
		fields["priority"] = "invalid"
	}
	if in.CategoryID != nil {
		var n int64
		if err := s.DB.WithContext(r.Context()).Model(&models.Category{}).Where("id = ? AND is_active", *in.CategoryID).Count(&n).Error; err != nil {
			slog.Error("read category", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if n == 0 {
			fields["category_id"] = "not_found"
		}
	}
	if len(fields) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": fields})
		return
	}

	actor, ip := audit.Staff(currentStaff(r).ID), s.clientIP(r)
	err := s.withLockedTicket(r, t.ID, func(tx *gorm.DB, t *models.Ticket) error {
		// Checked before any write, so a rejected status leaves priority and category unchanged too.
		if in.Status != nil && !staffTransitions[[2]string{t.Status, *in.Status}] {
			return errBadTransition
		}
		if from := optText(t.Priority); in.Priority != nil && from != *in.Priority {
			if err := tx.Model(t).Update("priority", *in.Priority).Error; err != nil {
				return err
			}
			if err := audit.Record(tx, actor, "ticket.priority_changed", audit.Change{TicketID: &t.ID, From: from, To: *in.Priority, IP: ip}); err != nil {
				return err
			}
		}
		if from, to := optText(t.CategoryID), optText(in.CategoryID); in.CategoryID != nil && from != to {
			if err := tx.Model(t).Update("category_id", *in.CategoryID).Error; err != nil {
				return err
			}
			if err := audit.Record(tx, actor, "ticket.category_changed", audit.Change{TicketID: &t.ID, From: from, To: to, IP: ip}); err != nil {
				return err
			}
		}
		if in.Status != nil {
			return setStatus(tx, t, *in.Status, actor, ip)
		}
		return nil
	})
	writeActionResult(w, err, "update ticket")
}

// PUT /api/staff/tickets/{id}/assignee {"assignee_id": 7 | null} — null unassigns. The status stays as it is.
func (s *Server) assignTicket(w http.ResponseWriter, r *http.Request) {
	t, ok := s.staffTicketByPath(w, r)
	if !ok {
		return
	}
	var in struct {
		AssigneeID json.RawMessage `json:"assignee_id"` // raw, so a missing field is not taken as null
	}
	if !decodeBody(w, r, &in) {
		return
	}
	var to *int64
	if len(in.AssigneeID) == 0 || json.Unmarshal(in.AssigneeID, &to) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"assignee_id": "invalid"}})
		return
	}

	me := currentStaff(r)
	db := s.DB.WithContext(r.Context())
	var role string
	if err := db.Raw("SELECT name FROM roles WHERE id = ?", me.RoleID).Scan(&role).Error; err != nil {
		slog.Error("read role", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// ponytail: self-only is checked by role name, as migration 00002 says the app enforces it for Agent.
	// A custom role with ticket.assign can assign anyone; add an "assign self only" role flag if that matters.
	if role == "Agent" && (to == nil || *to != me.ID) {
		writeError(w, http.StatusForbidden, "ticket.assign_self_only")
		return
	}
	if to != nil {
		var n int64
		if err := db.Model(&models.Staff{}).Where("id = ? AND is_active", *to).Count(&n).Error; err != nil {
			slog.Error("read staff", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if n == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"assignee_id": "not_found"}})
			return
		}
	}

	err := s.withLockedTicket(r, t.ID, func(tx *gorm.DB, t *models.Ticket) error {
		from, next := optText(t.AssigneeID), optText(to)
		if from == next {
			return nil
		}
		if err := tx.Model(t).Update("assignee_id", to).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Staff(me.ID), "ticket.assigned", audit.Change{TicketID: &t.ID, From: from, To: next, IP: s.clientIP(r)})
	})
	writeActionResult(w, err, "assign ticket")
}

// GET /api/staff/assignees — active staff for the assignee picker (T2.06), ordered by name.
func (s *Server) assignees(w http.ResponseWriter, r *http.Request) {
	out := []namedItem{}
	if err := s.DB.WithContext(r.Context()).Model(&models.Staff{}).Select("id", "name").Where("is_active").Order("name, id").Find(&out).Error; err != nil {
		slog.Error("list assignees", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/staff/tickets/{id}/comments {"body": "...", "internal": false} — a public reply or an internal
// note (FR-T6). The first public reply sets first_response_at; the status stays as it is.
func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	t, ok := s.staffTicketByPath(w, r)
	if !ok {
		return
	}
	var in struct {
		Body     string `json:"body"`
		Internal bool   `json:"internal"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	body := strings.TrimSpace(strings.ReplaceAll(in.Body, crlf, lf))
	if p := textProblem("case_details", body, 1, 5000); p != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation", "fields": map[string]string{"body": p}})
		return
	}

	me := currentStaff(r)
	visibility := "public"
	if in.Internal {
		visibility = "internal"
	}
	c := models.Comment{TicketID: t.ID, AuthorStaffID: &me.ID, Body: body, IsInternal: in.Internal}
	err := s.withLockedTicket(r, t.ID, func(tx *gorm.DB, t *models.Ticket) error {
		if t.Status == models.StatusClosed {
			return errTicketClosed
		}
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		if !in.Internal && t.FirstResponseAt == nil { // safe to check here: the row is locked
			if err := tx.Model(t).Update("first_response_at", gorm.Expr("now()")).Error; err != nil {
				return err
			}
		}
		return audit.Record(tx, audit.Staff(me.ID), "comment.added", audit.Change{TicketID: &t.ID, To: visibility, IP: s.clientIP(r)})
	})
	if err != nil {
		writeActionResult(w, err, "add comment")
		return
	}
	writeJSON(w, http.StatusCreated, staffComment{ID: c.ID, Author: &namedItem{ID: me.ID, Name: me.Name}, Body: c.Body, Internal: c.IsInternal, CreatedAt: c.CreatedAt})
}

// decodeBody reads a small JSON body with no unknown fields, or writes 400 invalid_body.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	return true
}

func writeActionResult(w http.ResponseWriter, err error, what string) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errBadTransition):
		writeError(w, http.StatusConflict, "ticket.bad_transition")
	case errors.Is(err, errTicketClosed):
		writeError(w, http.StatusConflict, "ticket.closed")
	case errors.Is(err, gorm.ErrRecordNotFound): // deleted since staffTicketByPath read it
		writeError(w, http.StatusNotFound, "ticket.not_found")
	default:
		slog.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

// optText is an optional value as audit text; "" (stored as NULL) when unset.
func optText[T any](p *T) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
