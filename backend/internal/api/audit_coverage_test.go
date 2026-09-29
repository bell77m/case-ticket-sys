package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

// routeRecorder collects the patterns Routes registers.
type routeRecorder []string

func (r *routeRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	*r = append(*r, pattern)
}

// wantAudit is one audit_log row a call must write: ticket is its ticket_id, target its target (zero values match any).
type wantAudit struct {
	action string
	ticket int64
	target string
}

// FR-L1: every mutating route writes its audit_log row. A new non-GET route without a case here fails the build.
func TestAuditCoverage_FRL1(t *testing.T) {
	photo := append(append([]byte{}, jpegHead...), make([]byte, 1000)...)
	cases := map[string]func(e *testEnv) []wantAudit{
		"POST /api/tickets": func(e *testEnv) []wantAudit {
			c := e.newTicket()
			return []wantAudit{{action: "ticket.created", ticket: c.TicketID}}
		},
		"POST /api/tickets/{id}/attachments": func(e *testEnv) []wantAudit {
			c := e.newTicket()
			if rec := e.upload(c.TicketID, c.TrackingToken, "a.jpg", photo); rec.Code != http.StatusCreated {
				e.t.Fatalf("upload = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "attachment.added", ticket: c.TicketID}}
		},
		"POST /api/track/comments": func(e *testEnv) []wantAudit {
			c := e.newTicket()
			e.setStatus(c.TicketID, models.StatusWaiting)
			if r := e.reply(c.TrackingToken, "Still broken after restart."); r.code != http.StatusCreated {
				e.t.Fatalf("reply = %d %s", r.code, r.body)
			}
			return []wantAudit{{action: "comment.added", ticket: c.TicketID}, {action: "ticket.status_changed", ticket: c.TicketID}}
		},
		"POST /api/track/confirm": func(e *testEnv) []wantAudit {
			c := e.newTicket()
			e.setStatus(c.TicketID, models.StatusResolved)
			if rec := e.do(http.MethodPost, "/api/track/confirm", c.TrackingToken, nil, ""); rec.Code != http.StatusOK {
				e.t.Fatalf("confirm = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "ticket.status_changed", ticket: c.TicketID}}
		},
		"PATCH /api/staff/tickets/{id}": func(e *testEnv) []wantAudit {
			c := e.newTicket()
			var cat int64
			e.db.Raw("SELECT min(id) FROM categories WHERE is_active").Scan(&cat)
			body := fmt.Sprintf(`{"status": "in_progress", "priority": "high", "category_id": %d}`, cat)
			if rec := e.sendJSON(http.MethodPatch, ticketPath(c.TicketID), e.session(e.newStaff("Team Lead", true)), body); rec.Code != http.StatusNoContent {
				e.t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "ticket.status_changed", ticket: c.TicketID},
				{action: "ticket.priority_changed", ticket: c.TicketID}, {action: "ticket.category_changed", ticket: c.TicketID}}
		},
		"PUT /api/staff/tickets/{id}/assignee": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true) // before the ticket: cleanups run last first, so the ticket goes first
			c := e.newTicket()
			body := fmt.Sprintf(`{"assignee_id": %d}`, st.ID)
			if rec := e.sendJSON(http.MethodPut, ticketPath(c.TicketID)+"/assignee", e.session(st), body); rec.Code != http.StatusNoContent {
				e.t.Fatalf("PUT assignee = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "ticket.assigned", ticket: c.TicketID}}
		},
		"POST /api/staff/tickets/{id}/comments": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true) // before the ticket, as above
			c := e.newTicket()
			if rec := e.sendJSON(http.MethodPost, ticketPath(c.TicketID)+"/comments", e.session(st), `{"body": "On my way."}`); rec.Code != http.StatusCreated {
				e.t.Fatalf("POST comment = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "comment.added", ticket: c.TicketID}}
		},
		"POST /api/staff/accounts": func(e *testEnv) []wantAudit {
			username := fmt.Sprintf("new%d", time.Now().UnixNano())
			body := fmt.Sprintf(`{"name": "New Agent", "username": %q, "role_id": %d, "password": %q}`, username, e.roleID("Agent"), testPassword)
			rec := e.sendJSON(http.MethodPost, "/api/staff/accounts", e.session(e.newStaff("Root Admin", true)), body)
			var acc staffAccount
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &acc) != nil {
				e.t.Fatalf("POST staff = %d %s", rec.Code, rec.Body)
			}
			e.t.Cleanup(func() { e.dropStaff(acc.ID) })
			return []wantAudit{{action: "staff.created", target: username}}
		},
		"PATCH /api/staff/accounts/{id}": func(e *testEnv) []wantAudit {
			c, st := e.session(e.newStaff("Admin", true)), e.newStaff("Agent", true)
			for _, body := range []string{fmt.Sprintf(`{"role_id": %d, "is_active": false}`, e.roleID("Team Lead")), `{"is_active": true}`} {
				if rec := e.sendJSON(http.MethodPatch, accountPath(st.ID), c, body); rec.Code != http.StatusOK {
					e.t.Fatalf("PATCH staff = %d %s", rec.Code, rec.Body)
				}
			}
			return []wantAudit{{action: "staff.role_changed", target: st.Username},
				{action: "staff.deactivated", target: st.Username}, {action: "staff.reactivated", target: st.Username}}
		},
		"POST /api/staff/roles": func(e *testEnv) []wantAudit {
			name := fmt.Sprintf("New role %d", time.Now().UnixNano())
			body := fmt.Sprintf(`{"name": %q, "permissions": ["ticket.view_all"]}`, name)
			rec := e.sendJSON(http.MethodPost, "/api/staff/roles", e.session(e.newStaff("Root Admin", true)), body)
			var role roleOutput
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &role) != nil {
				e.t.Fatalf("POST role = %d %s", rec.Code, rec.Body)
			}
			e.t.Cleanup(func() { e.db.Exec("DELETE FROM roles WHERE id = ?", role.ID) })
			return []wantAudit{{action: "role.created", target: name}}
		},
		"PUT /api/staff/roles/{id}/permissions": func(e *testEnv) []wantAudit {
			id, name := e.newRole() // before the staff member, as newRole asks
			c := e.session(e.newStaff("Root Admin", true))
			if rec := e.sendJSON(http.MethodPut, rolePermsPath(id), c, `{"permissions": ["ticket.view_all"]}`); rec.Code != http.StatusOK {
				e.t.Fatalf("PUT permissions = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "role.changed", target: name}}
		},
		"POST /api/staff/categories": func(e *testEnv) []wantAudit {
			en := fmt.Sprintf("New category %d", time.Now().UnixNano())
			rec := e.sendJSON(http.MethodPost, "/api/staff/categories", e.session(e.newStaff("Team Lead", true)), mustJSON(map[string]any{"name": names(en)}))
			var cat adminCategory
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &cat) != nil {
				e.t.Fatalf("POST category = %d %s", rec.Code, rec.Body)
			}
			e.t.Cleanup(func() { e.db.Exec("DELETE FROM categories WHERE id = ?", cat.ID) })
			return []wantAudit{{action: "category.created", target: en}}
		},
		"PATCH /api/staff/categories/{id}": func(e *testEnv) []wantAudit {
			en := fmt.Sprintf("Old category %d", time.Now().UnixNano())
			cat := models.Category{Name: names(en), IsActive: true}
			if err := e.db.Create(&cat).Error; err != nil {
				e.t.Fatal(err)
			}
			e.t.Cleanup(func() { e.db.Exec("DELETE FROM categories WHERE id = ?", cat.ID) })
			c, en2 := e.session(e.newStaff("Team Lead", true)), en+" renamed"
			for _, body := range []string{mustJSON(map[string]any{"name": names(en2), "is_active": false}), `{"is_active": true}`} {
				if rec := e.sendJSON(http.MethodPatch, categoryPath(cat.ID), c, body); rec.Code != http.StatusOK {
					e.t.Fatalf("PATCH category = %d %s", rec.Code, rec.Body)
				}
			}
			return []wantAudit{{action: "category.changed", target: en2},
				{action: "category.deactivated", target: en2}, {action: "category.reactivated", target: en2}}
		},
		"POST /api/staff/locations": func(e *testEnv) []wantAudit {
			b := fmt.Sprintf("New building %d", time.Now().UnixNano())
			body := mustJSON(map[string]any{"building": names(b), "floor": names("1"), "line": names("L1")})
			rec := e.sendJSON(http.MethodPost, "/api/staff/locations", e.session(e.newStaff("Team Lead", true)), body)
			var loc adminLocation
			if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &loc) != nil {
				e.t.Fatalf("POST location = %d %s", rec.Code, rec.Body)
			}
			e.t.Cleanup(func() { e.db.Exec("DELETE FROM locations WHERE id = ?", loc.ID) })
			return []wantAudit{{action: "location.created", target: b + " / 1 / L1"}}
		},
		"PATCH /api/staff/locations/{id}": func(e *testEnv) []wantAudit {
			b := fmt.Sprintf("Old building %d", time.Now().UnixNano())
			loc := models.Location{Building: names(b), Floor: names("1"), Line: names("L1"), IsActive: true}
			if err := e.db.Create(&loc).Error; err != nil {
				e.t.Fatal(err)
			}
			e.t.Cleanup(func() { e.db.Exec("DELETE FROM locations WHERE id = ?", loc.ID) })
			c, moved := e.session(e.newStaff("Team Lead", true)), b+" / 1 / L2"
			for _, body := range []string{mustJSON(map[string]any{"line": names("L2"), "is_active": false}), `{"is_active": true}`} {
				if rec := e.sendJSON(http.MethodPatch, locationPath(loc.ID), c, body); rec.Code != http.StatusOK {
					e.t.Fatalf("PATCH location = %d %s", rec.Code, rec.Body)
				}
			}
			return []wantAudit{{action: "location.changed", target: moved},
				{action: "location.deactivated", target: moved}, {action: "location.reactivated", target: moved}}
		},
		"PUT /api/staff/accounts/{id}/password": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true)
			if rec := e.sendJSON(http.MethodPut, accountPath(st.ID)+"/password", e.session(e.newStaff("Root Admin", true)), `{"password": "temporary-pass-1"}`); rec.Code != http.StatusNoContent {
				e.t.Fatalf("PUT password = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "staff.password_reset", target: st.Username}}
		},
		"POST /api/staff/reports/export": func(e *testEnv) []wantAudit {
			e.useGotenberg(newFakeGotenberg(e.t, http.StatusOK, fakePDF).url, "http://localhost:5173")
			body := `{"from": "2020-03-01", "to": "2020-03-10", "lang": "my"}`
			if rec := e.sendJSON(http.MethodPost, "/api/staff/reports/export", e.session(e.newStaff("Viewer", true)), body); rec.Code != http.StatusOK {
				e.t.Fatalf("POST export = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "report.exported",
				target: `{"building":"","category_id":"","from":"2020-03-01","lang":"my","to":"2020-03-10"}`}}
		},
		"POST /api/staff/activity/export": func(e *testEnv) []wantAudit {
			e.useGotenberg(newFakeGotenberg(e.t, http.StatusOK, fakePDF).url, "http://localhost:5173")
			body := `{"action": ["login.failed"], "from": "2020-03-01", "lang": "th"}`
			if rec := e.sendJSON(http.MethodPost, "/api/staff/activity/export", e.session(e.newStaff("Team Lead", true)), body); rec.Code != http.StatusOK {
				e.t.Fatalf("POST activity export = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "activity.exported", target: `{"action":["login.failed"],"from":"2020-03-01","tz":"UTC","lang":"th"}`}}
		},
		"POST /api/auth/password": func(e *testEnv) []wantAudit {
			st := e.newStaff("Agent", true)
			if rec := e.sendJSON(http.MethodPost, "/api/auth/password", e.session(st), `{"current_password": "`+testPassword+`", "new_password": "another-password-1"}`); rec.Code != http.StatusNoContent {
				e.t.Fatalf("POST password = %d %s", rec.Code, rec.Body)
			}
			return []wantAudit{{action: "staff.password_changed", target: st.Username}}
		},
		"POST /api/auth/login": func(e *testEnv) []wantAudit {
			st := e.newStaff("Viewer", true)
			if rec := e.login(st.Username, testPassword); rec.Code != http.StatusOK {
				e.t.Fatalf("login = %d %s", rec.Code, rec.Body)
			}
			nobody := fmt.Sprintf("nobody%d", time.Now().UnixNano())
			if rec := e.login(nobody, testPassword); rec.Code != http.StatusUnauthorized {
				e.t.Fatalf("unknown login = %d, want 401", rec.Code)
			}
			return []wantAudit{{action: "login.success", target: st.Username}, {action: "login.failed", target: nobody}}
		},
	}
	exempt := map[string]string{
		"POST /api/auth/logout": "deletes the Redis session only; no DB change, not an FR-L1 action",
	}

	var routes routeRecorder
	(&Server{}).Routes(&routes)
	registered := map[string]bool{}
	for _, p := range routes {
		registered[p] = true
		if _, ok := cases[p]; !ok && exempt[p] == "" && !strings.HasPrefix(p, "GET ") {
			t.Errorf("%s: no audit case (FR-L1); add one to TestAuditCoverage_FRL1", p)
		}
	}
	for p := range cases {
		if !registered[p] {
			t.Errorf("%s: audit case for a route that no longer exists", p)
		}
	}
	for p := range exempt {
		if !registered[p] {
			t.Errorf("%s: exempt route no longer exists", p)
		}
	}
	if t.Failed() {
		return
	}

	e := newAuthEnv(t)
	for p, call := range cases {
		t.Run(p, func(t *testing.T) {
			e.t = t
			var before int64
			e.db.Raw("SELECT coalesce(max(id), 0) FROM audit_log").Scan(&before)
			for _, w := range call(e) {
				q := e.db.Model(&models.AuditEntry{}).Where("id > ? AND action = ?", before, w.action)
				if w.ticket != 0 {
					q = q.Where("ticket_id = ?", w.ticket)
				}
				if w.target != "" {
					q = q.Where("target = ?", w.target)
				}
				var n int64
				if err := q.Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					t.Errorf("%s wrote no %+v audit row", p, w)
				}
			}
		})
	}
}
