package api

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ticket-app/internal/models"
)

var (
	jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}
	pngHead  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	exeHead  = []byte{'M', 'Z', 0x90, 0}
)

func ftyp(brand string) []byte {
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}, []byte(brand+"\x00\x00\x00\x00")...)
}

// NFR-5: the type comes from the file's bytes, never its name.
func TestSniffMedia_NFR5(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		want string // "" = rejected
	}{
		{"jpeg", jpegHead, "image"},
		{"png", pngHead, "image"},
		{"heic", ftyp("heic"), "image"},
		{"mp4", ftyp("isom"), "video"},
		{"mov", ftyp("qt  "), "video"},
		{"exe", exeHead, ""},
		{"pdf", []byte("%PDF-1.7"), ""},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		if got, _ := sniffMedia(tt.head); got != tt.want {
			t.Errorf("%s: sniffMedia = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// FR-T1, NFR-5: upload rules against the dev DB.
func TestUploadAttachment_FRT1(t *testing.T) {
	e := newTestEnv(t)
	created := e.newTicket()
	tok := created.TrackingToken
	photo := append(append([]byte{}, jpegHead...), bytes.Repeat([]byte{1}, 1000)...)

	if rec := e.upload(created.TicketID, "wrong-token", "a.jpg", photo); rec.Code != http.StatusNotFound {
		t.Errorf("wrong token: code = %d, want 404", rec.Code)
	}
	if rec := e.upload(created.TicketID, tok, "photo.jpg", exeHead); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("renamed exe: code = %d, want 415 (%s)", rec.Code, rec.Body)
	}
	big := append(append([]byte{}, jpegHead...), make([]byte, 10<<20)...) // 10 MB + header
	if rec := e.upload(created.TicketID, tok, "big.jpg", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("11 MB image: code = %d, want 413 (%s)", rec.Code, rec.Body)
	}
	for i := 1; i <= 5; i++ {
		if rec := e.upload(created.TicketID, tok, "p.jpg", photo); rec.Code != http.StatusCreated {
			t.Fatalf("upload %d: code = %d, want 201 (%s)", i, rec.Code, rec.Body)
		}
	}
	if rec := e.upload(created.TicketID, tok, "p.jpg", photo); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "file.limit_reached") {
		t.Errorf("6th file: code = %d, want 409 file.limit_reached (%s)", rec.Code, rec.Body)
	}

	var rows []models.Attachment
	e.db.Where("ticket_id = ?", created.TicketID).Find(&rows)
	if len(rows) != 5 {
		t.Fatalf("attachment rows = %d, want 5", len(rows))
	}
	entries, _ := os.ReadDir(e.dir + "/" + strconv.FormatInt(created.TicketID, 10))
	if len(entries) != 5 {
		t.Errorf("files on disk = %d, want 5 (rejected uploads must leave nothing behind)", len(entries))
	}
	for _, en := range entries {
		if strings.Contains(en.Name(), "photo") || strings.Contains(en.Name(), "p.jpg") {
			t.Errorf("stored name %q reuses the guest's file name, want a random name", en.Name())
		}
	}
	var audits int64
	e.db.Model(&models.AuditEntry{}).Where("ticket_id = ? AND action = 'attachment.added'", created.TicketID).Count(&audits)
	if audits != 5 {
		t.Errorf("attachment.added audit rows = %d, want 5", audits)
	}
}

// FR-T1: a closed ticket accepts no new evidence.
func TestUploadToClosedTicket_FRT1(t *testing.T) {
	e := newTestEnv(t)
	created := e.newTicket()
	e.setStatus(created.TicketID, models.StatusClosed)
	rec := e.upload(created.TicketID, created.TrackingToken, "a.jpg", jpegHead)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "ticket.closed") {
		t.Errorf("upload to closed ticket: %d %s, want 409 ticket.closed", rec.Code, rec.Body)
	}
}

// T2.14: the server cuts off requests after its ReadTimeout, but a guest's video upload on a slow phone link takes
// longer. The upload handler extends its own read deadline, so a slow but steady upload still succeeds.
func TestUploadOutlastsReadTimeout_FRT1(t *testing.T) {
	e := newTestEnv(t)
	c := e.newTicket()
	srv := httptest.NewUnstartedServer(e.mux)
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	body, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fw, _ := mw.CreateFormFile("file", "slow.jpg")
		_, _ = fw.Write(jpegHead)
		for range 8 { // about 800 ms in all, well past the 300 ms ReadTimeout
			time.Sleep(100 * time.Millisecond)
			_, _ = fw.Write(make([]byte, 1024))
		}
		_ = mw.Close()
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/tickets/"+strconv.FormatInt(c.TicketID, 10)+"/attachments", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Tracking-Token", c.TrackingToken)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("slow upload: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Errorf("slow upload = %d, want 201", res.StatusCode)
	}
}
