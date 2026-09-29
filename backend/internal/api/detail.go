package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"gorm.io/gorm"

	"ticket-app/internal/models"
)

// staffTicket is the ticket detail staff see (T1.19): guest details and internal notes, never the token hash.
type staffTicket struct {
	ID              int64             `json:"id"`
	Summary         string            `json:"summary"`
	CaseDetails     string            `json:"case_details"`
	Status          string            `json:"status"`
	Priority        *string           `json:"priority"`
	Category        *string           `json:"category"`
	CategoryID      *int64            `json:"category_id"`
	Location        trackLocation     `json:"location"`
	GuestName       string            `json:"guest_name"`
	EmployeeID      string            `json:"employee_id"`
	Language        string            `json:"language"`
	Assignee        *namedItem        `json:"assignee"`
	FirstResponseAt *time.Time        `json:"first_response_at"`
	ResolvedAt      *time.Time        `json:"resolved_at"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Comments        []staffComment    `json:"comments"`
	Attachments     []trackAttachment `json:"attachments"`
	Timeline        []timelineEvent   `json:"timeline"`
}

// timelineEvent is one audit_log row of the ticket with display values (FR-L3). Never the IP or target.
type timelineEvent struct {
	ID     int64  `json:"id"`
	Action string `json:"action"`
	Actor  struct {
		Type string `json:"type"`           // guest, staff or system
		Name string `json:"name,omitempty"` // staff only
	} `json:"actor"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type staffComment struct {
	ID        int64      `json:"id"`
	Author    *namedItem `json:"author"` // nil = the guest
	Body      string     `json:"body"`
	Internal  bool       `json:"internal"`
	CreatedAt time.Time  `json:"created_at"`
}

// staffTicketByPath loads the ticket named by {id}, or writes a 404.
func (s *Server) staffTicketByPath(w http.ResponseWriter, r *http.Request) (models.Ticket, bool) {
	var t models.Ticket
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.DB.WithContext(r.Context()).First(&t, id).Error
	}
	switch {
	case err == nil:
		return t, true
	case errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, strconv.ErrSyntax) || errors.Is(err, strconv.ErrRange):
		writeError(w, http.StatusNotFound, "ticket.not_found")
	default:
		slog.Error("read ticket", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
	return t, false
}

// GET /api/staff/tickets/{id}?lang=th
func (s *Server) staffTicket(w http.ResponseWriter, r *http.Request) {
	t, ok := s.staffTicketByPath(w, r)
	if !ok {
		return
	}
	db := s.DB.WithContext(r.Context())
	lang := r.URL.Query().Get("lang")
	out := staffTicket{ID: t.ID, Summary: t.Summary, CaseDetails: t.CaseDetails, Status: t.Status, Priority: t.Priority,
		CategoryID: t.CategoryID, GuestName: t.GuestName, EmployeeID: t.EmployeeID, Language: t.Language, FirstResponseAt: t.FirstResponseAt,
		ResolvedAt: t.ResolvedAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		Comments: []staffComment{}, Attachments: []trackAttachment{}, Timeline: []timelineEvent{}}

	var loc models.Location
	var comments []models.Comment
	var files []models.Attachment
	var events []models.AuditEntry
	err := errors.Join(
		db.First(&loc, t.LocationID).Error,
		db.Where("ticket_id = ?", t.ID).Order("created_at, id").Find(&comments).Error,
		db.Where("ticket_id = ?", t.ID).Order("id").Find(&files).Error,
		db.Where("ticket_id = ?", t.ID).Order("id").Find(&events).Error,
	)

	// Staff and category names the ticket, its comments and its timeline refer to: one query each.
	var staffIDs, catIDs []int64
	addID(&staffIDs, optText(t.AssigneeID))
	addID(&catIDs, optText(t.CategoryID))
	for _, c := range comments {
		addID(&staffIDs, optText(c.AuthorStaffID))
	}
	for _, ev := range events {
		auditRefs(ev, &staffIDs, &catIDs)
	}
	var names, catNames map[string]string
	if err == nil {
		names, catNames, err = nameMaps(db, staffIDs, catIDs, lang)
	}
	if err != nil {
		slog.Error("staff ticket", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	if name, ok := catNames[optText(t.CategoryID)]; ok {
		out.Category = &name
	}
	person := func(id *int64) *namedItem {
		if id == nil {
			return nil
		}
		return &namedItem{ID: *id, Name: names[optText(id)]}
	}
	out.Location = trackLocation{pickName(loc.Building, lang), pickName(loc.Floor, lang), pickName(loc.Line, lang)}
	out.Assignee = person(t.AssigneeID)
	for _, c := range comments {
		out.Comments = append(out.Comments, staffComment{ID: c.ID, Author: person(c.AuthorStaffID), Body: c.Body, Internal: c.IsInternal, CreatedAt: c.CreatedAt})
	}
	for _, f := range files {
		out.Attachments = append(out.Attachments, trackAttachment{ID: f.ID, MediaType: f.MediaType, SizeBytes: f.SizeBytes, CreatedAt: f.CreatedAt})
	}
	for _, ev := range events {
		te := timelineEvent{ID: ev.ID, Action: ev.Action, CreatedAt: ev.CreatedAt}
		te.From, te.To = auditValues(ev, names, catNames)
		te.Actor.Type = ev.ActorType
		if ev.ActorStaffID != nil {
			te.Actor.Name = names[optText(ev.ActorStaffID)]
		}
		out.Timeline = append(out.Timeline, te)
	}
	writeJSON(w, http.StatusOK, out)
}

// addID appends v, an ID as audit text, to dst; "" or anything else is skipped.
func addID(dst *[]int64, v string) {
	if id, err := strconv.ParseInt(v, 10, 64); err == nil {
		*dst = append(*dst, id)
	}
}

// auditRefs adds the staff and category IDs an audit row names: its actor, and both sides of an assignment or a
// category change.
func auditRefs(ev models.AuditEntry, staffIDs, catIDs *[]int64) {
	addID(staffIDs, optText(ev.ActorStaffID))
	switch ev.Action {
	case "ticket.assigned":
		addID(staffIDs, optText(ev.FromValue))
		addID(staffIDs, optText(ev.ToValue))
	case "ticket.category_changed":
		addID(catIDs, optText(ev.FromValue))
		addID(catIDs, optText(ev.ToValue))
	}
}

// auditValues is an audit row's old and new value for display: assignee and category IDs become names. Status and
// priority codes, and comment visibility, stay as stored: the frontend translates them.
func auditValues(ev models.AuditEntry, names, catNames map[string]string) (from, to string) {
	from, to = optText(ev.FromValue), optText(ev.ToValue)
	switch ev.Action {
	case "ticket.assigned":
		return lookup(names, from), lookup(names, to)
	case "ticket.category_changed":
		return lookup(catNames, from), lookup(catNames, to)
	}
	return from, to
}

// nameMaps reads staff names (deactivated staff too: history keeps their names) and category names in lang, one
// query each, keyed by ID as text like audit from/to values.
func nameMaps(db *gorm.DB, staffIDs, catIDs []int64, lang string) (names, catNames map[string]string, err error) {
	var staff []models.Staff
	var cats []models.Category
	if err := errors.Join(
		db.Select("id", "name").Where("id IN ?", staffIDs).Find(&staff).Error,
		db.Where("id IN ?", catIDs).Find(&cats).Error,
	); err != nil {
		return nil, nil, fmt.Errorf("names: %w", err)
	}
	names, catNames = map[string]string{}, map[string]string{}
	for _, st := range staff {
		names[strconv.FormatInt(st.ID, 10)] = st.Name
	}
	for _, c := range cats {
		catNames[strconv.FormatInt(c.ID, 10)] = pickName(c.Name, lang)
	}
	return names, catNames, nil
}

// lookup maps an ID stored as audit text to its display name; "" or an ID with no row left stays as stored.
func lookup(names map[string]string, v string) string {
	if n, ok := names[v]; ok {
		return n
	}
	return v
}

// GET /api/staff/tickets/{id}/attachments/{file} — same safe headers as the guest route (NFR-5).
func (s *Server) staffFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.staffTicketByPath(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("file"), 10, 64)
	var a models.Attachment
	if err != nil || s.DB.WithContext(r.Context()).Where("id = ? AND ticket_id = ?", id, t.ID).First(&a).Error != nil {
		writeError(w, http.StatusNotFound, "file.not_found")
		return
	}
	serveEvidence(w, r, filepath.Join(s.UploadDir, filepath.FromSlash(a.FilePath)))
}
