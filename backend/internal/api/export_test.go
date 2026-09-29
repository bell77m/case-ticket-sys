package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

const (
	fakePDF   = "%PDF-1.7 fake"
	printBase = "http://host.docker.internal:5173"
)

// fakeGotenberg stands in for Gotenberg: it records the last request's path and form fields and answers status, body.
type fakeGotenberg struct {
	url    string
	mu     sync.Mutex
	path   string
	fields map[string]string
}

func newFakeGotenberg(t *testing.T, status int, body string) *fakeGotenberg {
	g := &fakeGotenberg{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		g.path, g.fields = r.URL.Path, map[string]string{}
		for k, v := range r.MultipartForm.Value {
			g.fields[k] = v[0]
		}
		g.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	g.url = srv.URL
	return g
}

func (g *fakeGotenberg) last() (path string, fields map[string]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.path, g.fields
}

// useGotenberg rebuilds e's routes on a Server with the PDF export settings (FR-P4).
func (e *testEnv) useGotenberg(gotenbergURL, printBaseURL string) {
	e.mux = http.NewServeMux()
	(&Server{DB: e.db, UploadDir: e.dir, Sessions: e.sessions, GuestTicketLimit: 1000,
		GotenbergURL: gotenbergURL, PrintBaseURL: printBaseURL}).Routes(e.mux)
}

// printData calls GET /api/print/<kind> ("report" or "activity") with a print token, as the print page does.
func (e *testEnv) printData(kind, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/print/"+kind, nil)
	req.Header.Set("X-Print-Token", token)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// exportPaths are the PDF export routes by print kind.
var exportPaths = map[string]string{"report": "/api/staff/reports/export", "activity": "/api/staff/activity/export"}

// token is the print token Gotenberg was last sent, in the fragment of the /print/<kind> page URL.
func (g *fakeGotenberg) token(t *testing.T, kind string) string {
	t.Helper()
	_, fields := g.last()
	prefix := printBase + "/print/" + kind + "#"
	tok, ok := strings.CutPrefix(fields["url"], prefix)
	if !ok || len(tok) != 43 { // 32 bytes, base64url without padding
		t.Fatalf("url = %q, want %s<token>", fields["url"], prefix)
	}
	return tok
}

// exportToken posts an export of kind for c and returns the print token Gotenberg was sent.
func (e *testEnv) exportToken(g *fakeGotenberg, kind string, c *http.Cookie, body string) string {
	e.t.Helper()
	if rec := e.sendJSON(http.MethodPost, exportPaths[kind], c, body); rec.Code != http.StatusOK {
		e.t.Fatalf("export %s = %d %s", kind, rec.Code, rec.Body)
	}
	return g.token(e.t, kind)
}

// exportAudits lists the rows of an export action (report.exported, activity.exported) written after audit row since.
func (e *testEnv) exportAudits(action string, since int64) []models.AuditEntry {
	e.t.Helper()
	var rows []models.AuditEntry
	if err := e.db.Where("id > ? AND action = ?", since, action).Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// FR-P4, FR-L1, FR-I6: a Team Lead exports the report. Gotenberg gets the print page with a one-time token in the
// fragment and the viewer's language as the Paraglide cookie; the PDF comes back as an attachment, audited with the
// filters; the token gives the print page the same data as GET /api/staff/reports, once.
func TestReportExport_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	f := e.reportData()
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase+"/") // a trailing slash is dropped
	lead := e.newStaff("Team Lead", true)
	before := e.lastAuditID()
	cat := strconv.FormatInt(f.cat, 10)
	filters := map[string]string{"from": "2020-03-01", "to": "2020-03-10", "tz": "Asia/Bangkok", "building": f.building, "category_id": cat}
	body := maps.Clone(filters)
	body["lang"] = "th"

	rec := e.sendJSON(http.MethodPost, "/api/staff/reports/export", e.session(lead), mustJSON(body))
	today := time.Now().UTC().Add(7 * time.Hour).Format(time.DateOnly) // Bangkok is UTC+7 all year
	for _, h := range []struct{ name, want string }{
		{"Content-Type", "application/pdf"},
		{"Content-Disposition", `attachment; filename="tickets-report-` + today + `.pdf"`},
		{"Cache-Control", "no-store"},
	} {
		if got := rec.Header().Get(h.name); got != h.want {
			t.Errorf("%s = %q, want %q", h.name, got, h.want)
		}
	}
	if rec.Code != http.StatusOK || rec.Body.String() != fakePDF {
		t.Fatalf("export = %d %q, want 200 %q", rec.Code, rec.Body, fakePDF)
	}

	t.Run("audit FR-L1", func(t *testing.T) {
		rows := e.exportAudits("report.exported", before)
		want := mustJSON(map[string]string{"from": "2020-03-01", "to": "2020-03-10", "building": f.building, "category_id": cat, "lang": "th"})
		if len(rows) != 1 || rows[0].ActorStaffID == nil || *rows[0].ActorStaffID != lead.ID || rows[0].Target == nil || *rows[0].Target != want {
			t.Fatalf("report.exported rows = %+v, want one by staff %d with target %s", rows, lead.ID, want)
		}
	})

	path, fields := g.last()
	token := g.token(t, "report")
	t.Run("gotenberg fields", func(t *testing.T) {
		if path != "/forms/chromium/convert/url" {
			t.Errorf("path = %q", path)
		}
		for _, f := range []struct{ name, want string }{
			{"waitForExpression", "window.printReady === true"},
			{"printBackground", "true"},
			{"emulatedMediaType", "print"},
			{"preferCssPageSize", "true"},
			{"paperWidth", "8.27"},
			{"paperHeight", "11.7"},
			{"failOnConsoleExceptions", "true"}, // a print page that fails (expired token) throws: no PDF, no audit row
		} {
			if got := fields[f.name]; got != f.want {
				t.Errorf("%s = %q, want %q", f.name, got, f.want)
			}
		}
		var cookies []map[string]any
		if err := json.Unmarshal([]byte(fields["cookies"]), &cookies); err != nil || len(cookies) != 1 ||
			cookies[0]["name"] != "PARAGLIDE_LOCALE" || cookies[0]["value"] != "th" || cookies[0]["domain"] != "host.docker.internal" {
			t.Errorf("cookies = %s, want the PARAGLIDE_LOCALE cookie th for host.docker.internal (FR-I6)", fields["cookies"])
		}
	})

	t.Run("token in Redis", func(t *testing.T) {
		sum := sha256.Sum256([]byte(token))
		key := "print:report:" + hex.EncodeToString(sum[:]) // the kind in the key keeps the token off /api/print/activity
		rdb, ctx := e.sessions.Redis, context.Background()
		if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 || ttl > time.Minute {
			t.Errorf("TTL = %v, want at most 60 s", ttl)
		}
		if v := rdb.Get(ctx, key).Val(); v == "" || strings.Contains(v, token) {
			t.Errorf("stored %q; want the job, never the raw token", v)
		}
		if rdb.Exists(ctx, "print:"+token).Val() != 0 {
			t.Error("raw token stored as a key")
		}
	})

	t.Run("print page data, once", func(t *testing.T) {
		rec := e.printData("report", token)
		var got map[string]json.RawMessage
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("print = %d %s", rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		if string(got["lang"]) != `"th"` {
			t.Errorf("lang = %s, want \"th\"", got["lang"])
		}
		if _, ok := got["filters"]; !ok {
			t.Error("no filters") // their values: TestPrintReportFilters_FRP4
		}
		delete(got, "lang")
		delete(got, "filters")
		q := url.Values{"lang": {"th"}}
		for k, v := range filters {
			q.Set(k, v)
		}
		want := e.getReport(f.viewer, q.Encode())
		if !maps.EqualFunc(got, want, func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }) {
			t.Errorf("print data =\n%s\nwant GET /api/staff/reports =\n%s", mustJSON(got), mustJSON(want))
		}
		if rec := e.printData("report", token); rec.Code != http.StatusNotFound || rec.Body.String() != `{"error":"print.not_found"}`+"\n" {
			t.Errorf("second use = %d %s, want 404 print.not_found", rec.Code, rec.Body)
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		for _, tok := range []string{"", "not-a-token"} {
			if rec := e.printData("report", tok); rec.Code != http.StatusNotFound {
				t.Errorf("token %q = %d %s, want 404", tok, rec.Code, rec.Body)
			}
		}
	})
}

// FR-P4, FR-I6: the print data says which filters the PDF shows (the current view): the resolved period, and the
// category's name in the stored language with the English fallback; unset filters are null.
func TestPrintReportFilters_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	f := e.reportData()
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	c := e.session(e.newStaff("Viewer", true))
	n, cat := f.building[len("Report test "):], itoa(f.cat)
	tests := []struct{ name, body, want string }{
		{"all set, Thai", mustJSON(map[string]string{"from": "2020-03-01", "to": "2020-03-10", "tz": "Asia/Bangkok",
			"building": f.building, "category_id": cat, "lang": "th"}),
			`{"from":"2020-03-01","to":"2020-03-10","tz":"Asia/Bangkok","building":"ตึกรายงาน ` + n +
				`","category":"หมวดรายงาน ` + n + `","category_id":` + cat + `}`},
		{"no Burmese names", mustJSON(map[string]string{"from": "2020-03-01", "to": "2020-03-10", "building": f.building, "category_id": cat, "lang": "my"}),
			`{"from":"2020-03-01","to":"2020-03-10","tz":"UTC","building":` + mustJSON(f.building) +
				`,"category":"Report cat ` + n + `","category_id":` + cat + `}`},
		{"unknown building", mustJSON(map[string]string{"from": "2020-03-01", "to": "2020-03-10", "building": "No such building " + n, "lang": "th"}),
			`{"from":"2020-03-01","to":"2020-03-10","tz":"UTC","building":"No such building ` + n + `","category":null,"category_id":null}`},
		{"none set", `{"from": "2020-03-01", "to": "2020-03-10", "lang": "en"}`,
			`{"from":"2020-03-01","to":"2020-03-10","tz":"UTC","building":null,"category":null,"category_id":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			rec := e.printData("report", e.exportToken(g, "report", c, tt.body))
			var got struct{ Filters json.RawMessage }
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || string(got.Filters) != tt.want {
				t.Errorf("print = %d filters %s\nwant %s", rec.Code, got.Filters, tt.want)
			}
		})
	}
}

// FR-P4, FR-R3: export needs report.view; the print data needs no session, only the token.
func TestReportExportPermission_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	tests := []struct {
		name string
		c    *http.Cookie
		want int
	}{
		{"Agent", e.session(e.newStaff("Agent", true)), http.StatusForbidden},
		{"Viewer", e.session(e.newStaff("Viewer", true)), http.StatusOK},
		{"no session", nil, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := e.sendJSON(http.MethodPost, "/api/staff/reports/export", tt.c, `{"lang": "en"}`); rec.Code != tt.want {
				t.Errorf("= %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}

// FR-P4, FR-L1: no PDF settings gives 503; a Gotenberg failure gives 502 and writes no audit row.
func TestReportExportFailures_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	c := e.session(e.newStaff("Team Lead", true))
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	tests := []struct {
		name, gotenberg, printBase string
		want                       int
		code                       string
	}{
		{"no settings", "", "", http.StatusServiceUnavailable, "pdf.unavailable"},
		{"no print base", newFakeGotenberg(t, http.StatusOK, fakePDF).url, "", http.StatusServiceUnavailable, "pdf.unavailable"},
		{"no gotenberg", "", printBase, http.StatusServiceUnavailable, "pdf.unavailable"},
		{"gotenberg down", down.URL, printBase, http.StatusBadGateway, "pdf.failed"},
		{"gotenberg error", newFakeGotenberg(t, http.StatusServiceUnavailable, "busy").url, printBase, http.StatusBadGateway, "pdf.failed"},
		// Gotenberg's answer when the print page throws (failOnConsoleExceptions), for example on an expired token.
		{"print page failed", newFakeGotenberg(t, http.StatusConflict, "Chromium console exceptions").url, printBase, http.StatusBadGateway, "pdf.failed"},
		{"not a PDF", newFakeGotenberg(t, http.StatusOK, "<html>").url, printBase, http.StatusBadGateway, "pdf.failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e.t = t
			e.useGotenberg(tt.gotenberg, tt.printBase)
			before := e.lastAuditID()
			rec := e.sendJSON(http.MethodPost, "/api/staff/reports/export", c, `{"lang": "en"}`)
			if code, _ := errorBody(rec); rec.Code != tt.want || code != tt.code {
				t.Errorf("= %d %s, want %d %s", rec.Code, rec.Body, tt.want, tt.code)
			}
			if rows := e.exportAudits("report.exported", before); len(rows) != 0 {
				t.Errorf("wrote %d report.exported rows, want none", len(rows))
			}
		})
	}
}

// FR-P2, FR-P4: the export checks its filters exactly as GET /api/staff/reports does, plus the language.
func TestReportExportValidation_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	c := e.session(e.newStaff("Viewer", true))
	tests := []struct{ body, want string }{
		{`{"lang": "de"}`, `{"error":"validation","fields":{"lang":"invalid"}}`},
		{`{}`, `{"error":"validation","fields":{"lang":"invalid"}}`},
		{`{"lang": "en", "status": "new"}`, `{"error":"invalid_body"}`},
		{`{"lang": "en", "category_id": 2}`, `{"error":"invalid_body"}`}, // filters are strings, as in the query
		{``, `{"error":"invalid_body"}`},
	}
	for _, tt := range reportValidationCases {
		q, _ := url.ParseQuery(tt.query)
		body := map[string]string{"lang": "en"}
		for k := range q {
			body[k] = q.Get(k)
		}
		tests = append(tests, struct{ body, want string }{mustJSON(body), `{"error":"validation","fields":` + tt.want + `}`})
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			rec := e.sendJSON(http.MethodPost, "/api/staff/reports/export", c, tt.body)
			if rec.Code != http.StatusBadRequest || rec.Body.String() != tt.want+"\n" {
				t.Errorf("= %d %s, want 400 %s", rec.Code, rec.Body, tt.want)
			}
		})
	}
	if path, _ := g.last(); path != "" {
		t.Errorf("Gotenberg was called (%s) for a bad request", path)
	}
}

// FR-P4, NFR-9, FR-R3: a print token stops working once its staff member is deactivated or loses the permission
// its export needs: report.view for the report, audit.view for the activity log (T3.06).
func TestPrintRevoked_FRP4(t *testing.T) {
	e := newAuthEnv(t)
	g := newFakeGotenberg(t, http.StatusOK, fakePDF)
	e.useGotenberg(g.url, printBase)
	tests := []struct {
		name   string
		revoke func(st models.Staff, roleID int64) string
	}{
		{"deactivated", func(st models.Staff, _ int64) string {
			return "UPDATE staff SET is_active = false WHERE id = " + itoa(st.ID)
		}},
		{"lost the permission", func(_ models.Staff, roleID int64) string {
			return "DELETE FROM role_permissions WHERE role_id = " + itoa(roleID)
		}},
		{"must change password", func(st models.Staff, _ int64) string {
			return "UPDATE staff SET must_change_password = true WHERE id = " + itoa(st.ID)
		}},
	}
	for kind, perm := range map[string]string{"report": rbac.ReportView, "activity": rbac.AuditView} {
		for _, tt := range tests {
			t.Run(kind+"/"+tt.name, func(t *testing.T) {
				e.t = t
				roleID, role := e.newRole() // before the staff member, as newRole asks
				if err := e.db.Exec("INSERT INTO role_permissions VALUES (?, ?)", roleID, perm).Error; err != nil {
					t.Fatal(err)
				}
				st := e.newStaff(role, true)
				token := e.exportToken(g, kind, e.session(st), `{"lang": "en"}`)
				if err := e.db.Exec(tt.revoke(st, roleID)).Error; err != nil {
					t.Fatal(err)
				}
				if rec := e.printData(kind, token); rec.Code != http.StatusNotFound {
					t.Errorf("print = %d %s, want 404", rec.Code, rec.Body)
				}
			})
		}
	}
}
