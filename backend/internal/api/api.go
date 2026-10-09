// Package api holds the HTTP handlers under /api.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"

	"gorm.io/gorm"

	"ticket-app/internal/auth"
	"ticket-app/internal/events"
	"ticket-app/internal/rbac"
)

// Server carries the dependencies shared by handlers.
type Server struct {
	DB            *gorm.DB
	UploadDir     string
	Sessions      *auth.Sessions
	SecureCookies bool // BASE_URL is https, so cookies get the Secure flag
	// GuestTicketLimit is guest tickets per IP per 10 minutes (NFR-3); 0 means 5.
	GuestTicketLimit int
	// GotenbergURL and PrintBaseURL enable the PDF export (FR-P4); if either is empty it answers 503.
	GotenbergURL string
	PrintBaseURL string
	// TrustedProxies are the ingress CIDRs whose X-Forwarded-For clientIP believes (FR-A11, NFR-3); nil trusts nobody.
	TrustedProxies []netip.Prefix
	// ClamdAddr is clamd's host:port; every upload is scanned there before it is kept (NFR-5). Empty refuses uploads.
	ClamdAddr string
}

// Routes registers every /api route on mux (an *http.ServeMux; tests pass a recorder to list the routes).
func (s *Server) Routes(mux interface {
	HandleFunc(string, func(http.ResponseWriter, *http.Request))
}) {
	mux.HandleFunc("GET /api/categories", s.categories)
	mux.HandleFunc("GET /api/locations", s.locations)
	mux.HandleFunc("POST /api/tickets", s.createTicket)
	mux.HandleFunc("POST /api/tickets/{id}/attachments", s.uploadAttachment)
	mux.HandleFunc("GET /api/track", s.trackView)
	mux.HandleFunc("POST /api/track/comments", s.trackReply)
	mux.HandleFunc("POST /api/track/confirm", s.trackConfirm)
	mux.HandleFunc("GET /api/track/attachments/{id}", s.trackFile)

	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/me", s.requireSession(s.me))
	mux.HandleFunc("POST /api/auth/password", s.requireSession(s.changePassword)) // works with a temporary password (FR-A8)
	mux.HandleFunc("GET /api/print/report", s.printReport)                        // Gotenberg has no session; a one-time token instead (FR-P4)
	mux.HandleFunc("GET /api/print/activity", s.printActivity)                    // same, for the activity log PDF

	// Staff endpoints: every one goes through require(permission) (FR-R3).
	mux.HandleFunc("GET /api/staff/tickets", s.require(rbac.TicketViewAll, s.queue))
	mux.HandleFunc("GET /api/staff/tickets/{id}", s.require(rbac.TicketViewAll, s.staffTicket))
	mux.HandleFunc("GET /api/staff/tickets/{id}/attachments/{file}", s.require(rbac.TicketViewAll, s.staffFile))
	mux.HandleFunc("PATCH /api/staff/tickets/{id}", s.require(rbac.TicketUpdate, s.updateTicket))
	mux.HandleFunc("PUT /api/staff/tickets/{id}/assignee", s.require(rbac.TicketAssign, s.assignTicket))
	mux.HandleFunc("GET /api/staff/assignees", s.require(rbac.TicketAssign, s.assignees))
	mux.HandleFunc("POST /api/staff/tickets/{id}/comments", s.require(rbac.TicketComment, s.addComment))
	mux.HandleFunc("GET /api/staff/reports", s.require(rbac.ReportView, s.reports))
	mux.HandleFunc("POST /api/staff/reports/export", s.require(rbac.ReportView, s.exportReport))
	mux.HandleFunc("GET /api/staff/activity", s.require(rbac.AuditView, s.activity))
	mux.HandleFunc("POST /api/staff/activity/export", s.require(rbac.AuditView, s.exportActivity))
	mux.HandleFunc("GET /api/staff/accounts", s.require(rbac.StaffManage, s.staffAccounts))
	mux.HandleFunc("POST /api/staff/accounts", s.require(rbac.StaffCreate, s.createStaff)) // Root Admin only (FR-A1)
	mux.HandleFunc("PATCH /api/staff/accounts/{id}", s.require(rbac.StaffManage, s.updateStaff))
	mux.HandleFunc("PUT /api/staff/accounts/{id}/password", s.require(rbac.StaffCreate, s.resetPassword)) // Root Admin only (FR-A9)
	mux.HandleFunc("GET /api/staff/roles", s.require(rbac.StaffManage, s.roles))
	mux.HandleFunc("POST /api/staff/roles", s.require(rbac.RoleManage, s.createRole))                         // Root Admin only (FR-A3)
	mux.HandleFunc("PUT /api/staff/roles/{id}/permissions", s.require(rbac.RoleManage, s.setRolePermissions)) // Root Admin only (FR-A3)
	mux.HandleFunc("GET /api/staff/categories", s.require(rbac.CategoryManage, s.adminCategories))
	mux.HandleFunc("POST /api/staff/categories", s.require(rbac.CategoryManage, s.createCategory))
	mux.HandleFunc("PATCH /api/staff/categories/{id}", s.require(rbac.CategoryManage, s.updateCategory))
	mux.HandleFunc("GET /api/staff/locations", s.require(rbac.CategoryManage, s.adminLocations))
	mux.HandleFunc("POST /api/staff/locations", s.require(rbac.CategoryManage, s.createLocation))
	mux.HandleFunc("PATCH /api/staff/locations/{id}", s.require(rbac.CategoryManage, s.updateLocation))
	mux.HandleFunc("GET /api/events", s.require(rbac.TicketViewAll, s.eventStream)) // every default role has it
}

// GET /api/events — server-sent ticket events for open staff pages (FR-P3). Each heartbeat re-checks what require
// checked, so deactivation or a lost permission ends the stream (NFR-9, FR-R3).
func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	events.Serve(w, r, s.Sessions.Redis, func() bool { return s.stillAllowed(r, rbac.TicketViewAll) })
}

// publish tells open staff pages that ticket id changed (FR-P3). Call it after the commit. events.Publish logs a
// failure; the request still succeeds, and pages show the change on their next load.
func (s *Server) publish(ctx context.Context, id int64) {
	_ = events.Publish(context.WithoutCancel(ctx), s.Sessions.Redis, id) // the change is committed even if the client left
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// writeError sends an error code; the frontend translates it (CLAUDE.md "i18n").
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
