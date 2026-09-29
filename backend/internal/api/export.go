package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
	"ticket-app/internal/rbac"
)

// PDF export (FR-P4): Gotenberg's Chromium opens the frontend's /print/<kind> page (kind "report" or "activity") with a
// one-time token in the URL fragment (never sent to a server, so no access log sees it); the page reads its data from
// GET /api/print/<kind>.
const (
	printTokenTTL = time.Minute
	pdfTimeout    = time.Minute
	maxPDFBytes   = 20 << 20
	// localeCookie is Paraglide's cookieName (frontend/src/lib/paraglide/runtime.js); it sets the print page's language (FR-I6).
	localeCookie = "PARAGLIDE_LOCALE"
)

// printJob is what a print token unlocks, stored in Redis under printKey. F is its kind's filters.
type printJob[F any] struct {
	StaffID int64  `json:"staff_id"`
	Filters F      `json:"filters"`
	Lang    string `json:"lang"`
}

// printKey is the Redis key of a print token of kind; only its SHA-256 is stored, like tracking tokens (NFR-2).
// The kind in the key keeps a token off the other kind's print endpoint.
func printKey(kind, token string) string {
	sum := sha256.Sum256([]byte(token))
	return "print:" + kind + ":" + hex.EncodeToString(sum[:])
}

// POST /api/staff/reports/export {"from", "to", "tz", "building", "category_id", "lang"} — the report as
// tickets-report-YYYY-MM-DD.pdf (FR-P4, FR-I6). Filters are strings, checked as GET /api/staff/reports checks them.
func (s *Server) exportReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		reportFilters
		Lang string `json:"lang"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	q, fields, err := s.checkReportFilters(r.Context(), in.reportFilters)
	if err != nil {
		s.reportFailed(w, err)
		return
	}
	if !languages[in.Lang] {
		fields["lang"] = "invalid"
	}
	if validationFailed(w, fields) {
		return
	}
	// The period as resolved, so the print page and the audit row show the dates used even when they were defaults.
	f := in.reportFilters
	f.From, f.To, f.TZ = q.from.Format(time.DateOnly), q.to.Format(time.DateOnly), q.tz
	target, _ := json.Marshal(map[string]string{"from": f.From, "to": f.To, "building": f.Building, "category_id": f.CategoryID, "lang": in.Lang})
	s.exportPDF(w, r, "report", f, in.Lang, "report.exported", string(target), "tickets-report-"+q.today+".pdf")
}

// exportPDF stores a print job of kind for the signed-in staff member under a one-time token, has Gotenberg print
// /print/<kind> with it, audits action with target, then sends the PDF as file (FR-P4). The audit row is committed
// before the first byte of the PDF goes out (FR-L1). The caller has checked filters and lang.
func (s *Server) exportPDF(w http.ResponseWriter, r *http.Request, kind string, filters any, lang, action, target, file string) {
	if s.GotenbergURL == "" || s.PrintBaseURL == "" {
		writeError(w, http.StatusServiceUnavailable, "pdf.unavailable")
		return
	}
	ctx, me := r.Context(), currentStaff(r)
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // never fails (crypto/rand)
	token := base64.RawURLEncoding.EncodeToString(raw)
	job, err := json.Marshal(printJob[any]{StaffID: me.ID, Filters: filters, Lang: lang})
	if err == nil {
		err = s.Sessions.Redis.Set(ctx, printKey(kind, token), job, printTokenTTL).Err()
	}
	if err != nil {
		internalError(w, "pdf export", fmt.Errorf("store print token: %w", err))
		return
	}
	pdf, err := s.renderPDF(ctx, kind, token, lang)
	if err != nil {
		slog.Error("pdf export", "error", err) // err never holds the token: it only travels in the request body
		writeError(w, http.StatusBadGateway, "pdf.failed")
		return
	}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return audit.Record(tx, audit.Staff(me.ID), action, audit.Change{Target: target})
	})
	if err != nil { // no PDF without its audit row (FR-L1)
		internalError(w, "pdf export", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", `attachment; filename="`+file+`"`)
	h.Set("Content-Length", strconv.Itoa(len(pdf)))
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(pdf)
}

// internalError logs err under what and answers 500 internal.
func internalError(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "error", err)
	writeError(w, http.StatusInternalServerError, "internal")
}

// renderPDF has Gotenberg print the frontend's /print/<kind> page for token, in lang (FR-P4, FR-I6).
func (s *Server) renderPDF(ctx context.Context, kind, token, lang string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pdfTimeout)
	defer cancel()
	base := strings.TrimSuffix(s.PrintBaseURL, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("PRINT_BASE_URL: %w", err)
	}
	cookies, _ := json.Marshal([]map[string]string{{"name": localeCookie, "value": lang, "domain": u.Hostname(), "path": "/"}})
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range [][2]string{
		{"url", base + "/print/" + kind + "#" + token},
		{"waitForExpression", "window.printReady === true"}, // the print page sets it once charts and fonts are drawn
		// A print page that fails (expired token) throws after showing its error: Gotenberg answers 409, so no PDF
		// and no audit row.
		{"failOnConsoleExceptions", "true"},
		{"printBackground", "true"},
		{"emulatedMediaType", "print"},
		{"preferCssPageSize", "true"},
		{"paperWidth", "8.27"}, {"paperHeight", "11.7"}, // A4, in inches
		{"marginTop", "0.4"}, {"marginBottom", "0.4"}, {"marginLeft", "0.4"}, {"marginRight", "0.4"},
		{"cookies", string(cookies)},
	} {
		_ = mw.WriteField(f[0], f[1]) // writes to a bytes.Buffer never fail
	}
	_ = mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(s.GotenbergURL, "/")+"/forms/chromium/convert/url", &body)
	if err != nil {
		return nil, fmt.Errorf("gotenberg request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gotenberg: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gotenberg: status %d", resp.StatusCode)
	}
	pdf, err := io.ReadAll(io.LimitReader(resp.Body, maxPDFBytes+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("gotenberg: read: %w", err)
	case len(pdf) > maxPDFBytes:
		return nil, errors.New("gotenberg: PDF over 20 MB")
	case !bytes.HasPrefix(pdf, []byte("%PDF-")):
		return nil, errors.New("gotenberg: answer is not a PDF")
	}
	return pdf, nil
}

// readPrintJob takes the print job of kind named by the X-Print-Token header, for a print page in Gotenberg, which
// has no session (FR-P4). The token works once (GETDEL) and only while its staff member still holds permission and
// could sign in (NFR-9, FR-R3); otherwise it writes 404 print.not_found, or 500, and ok is false.
func readPrintJob[F any](s *Server, w http.ResponseWriter, r *http.Request, kind, permission string) (job printJob[F], ok bool) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	raw, err := s.Sessions.Redis.GetDel(ctx, printKey(kind, r.Header.Get("X-Print-Token"))).Bytes()
	if errors.Is(err, redis.Nil) {
		writeError(w, http.StatusNotFound, "print.not_found")
		return job, false
	}
	if err == nil {
		err = json.Unmarshal(raw, &job)
	}
	var allowed bool // what require(permission) checks, without a session
	if err == nil {
		err = s.DB.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM staff s JOIN role_permissions p ON p.role_id = s.role_id
			WHERE s.id = ? AND s.is_active AND NOT s.must_change_password AND p.permission = ?)`, job.StaffID, permission).Scan(&allowed).Error
	}
	if err != nil {
		internalError(w, "print data", fmt.Errorf("read print token: %w", err))
		return job, false
	}
	if !allowed {
		writeError(w, http.StatusNotFound, "print.not_found")
		return job, false
	}
	return job, true
}

// GET /api/print/report with X-Print-Token — the report for the print page. The answer is GET /api/staff/reports's
// for the stored filters, plus "filters" and "lang".
func (s *Server) printReport(w http.ResponseWriter, r *http.Request) {
	job, ok := readPrintJob[reportFilters](s, w, r, "report", rbac.ReportView)
	if !ok {
		return
	}
	ctx := r.Context()
	q, fields, err := s.checkReportFilters(ctx, job.Filters)
	if err == nil && len(fields) > 0 {
		err = fmt.Errorf("stored print filters invalid: %v", fields)
	}
	if err != nil {
		s.reportFailed(w, err)
		return
	}
	out, err := s.buildReport(ctx, q, job.Lang)
	if err != nil {
		s.reportFailed(w, err)
		return
	}
	// The filters the PDF shows (the current view), in the viewer's language (FR-I6); unset ones are null.
	db := s.DB.WithContext(ctx)
	pf := printFilters{From: job.Filters.From, To: job.Filters.To, TZ: q.tz}
	if q.building != "" { // the filter is the English name; any row of that building, active or not, has its names
		name := q.building
		var loc models.Location
		if err := db.Where("building->>'en' = ?", q.building).Order("id").Limit(1).Find(&loc).Error; err != nil {
			s.reportFailed(w, err)
			return
		}
		if loc.ID != 0 {
			name = pickName(loc.Building, job.Lang)
		}
		pf.Building = &name
	}
	if q.category != 0 {
		var cat models.Category
		if err := db.Limit(1).Find(&cat, q.category).Error; err != nil {
			s.reportFailed(w, err)
			return
		}
		pf.CategoryID = &q.category
		if cat.ID != 0 {
			name := pickName(cat.Name, job.Lang)
			pf.Category = &name
		}
	}
	writeJSON(w, http.StatusOK, struct {
		reportOut
		Filters printFilters `json:"filters"`
		Lang    string       `json:"lang"`
	}{out, pf, job.Lang})
}

// printFilters are the filters as the print page shows them (FR-P4): category is its name in the viewer's language.
type printFilters struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	TZ         string  `json:"tz"`
	Building   *string `json:"building"`
	Category   *string `json:"category"`
	CategoryID *int64  `json:"category_id"`
}
