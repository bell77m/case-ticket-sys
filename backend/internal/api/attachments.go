package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticket-app/internal/audit"
	"ticket-app/internal/models"
)

// Evidence limits (FR-T1).
const (
	maxImageBytes     = 10 << 20
	maxVideoBytes     = 100 << 20
	maxFilesPerTicket = 5
	// uploadReadTimeout lets 100 MB through at about 1 Mbit/s.
	uploadReadTimeout = 15 * time.Minute
)

// sniffMedia returns the kind ("image", "video", or "" when rejected) and exact MIME type
// from a file's first bytes (NFR-5). Accepted: JPEG, PNG, HEIC/HEIF, MP4 and MOV.
// The file name and the client's Content-Type are ignored, on upload and when serving.
func sniffMedia(head []byte) (kind, mime string) {
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return "image", "image/jpeg"
	case bytes.HasPrefix(head, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image", "image/png"
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		switch string(head[8:12]) {
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1", "msf1":
			return "image", "image/heic"
		case "qt  ":
			return "video", "video/quicktime"
		case "isom", "iso2", "mp41", "mp42", "avc1", "M4V ", "mp4v", "dash":
			return "video", "video/mp4"
		}
	}
	return "", ""
}

var (
	errLimitReached = errors.New("attachment limit reached")
	errTicketClosed = errors.New("ticket closed")
)

// POST /api/tickets/{id}/attachments — a guest adds one evidence file (FR-T1).
// The guest proves the ticket is theirs with the tracking token in X-Tracking-Token.
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	ticketID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "ticket.not_found")
		return
	}
	t, ok := s.guestTicket(w, r)
	if !ok {
		return
	}
	if t.ID != ticketID {
		writeError(w, http.StatusNotFound, "ticket.not_found")
		return
	}
	if t.Status == models.StatusClosed {
		writeError(w, http.StatusConflict, "ticket.closed")
		return
	}
	// A 100 MB video on a slow phone link takes minutes, longer than the server's ReadTimeout. Only a caller with a
	// valid tracking token gets here, so the longer deadline is not open to anyone. (httptest recorders cannot set it.)
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadReadTimeout))
	// Cheap early check so a full ticket costs no disk write; re-checked under a row lock below.
	var existing int64
	if err := s.DB.WithContext(r.Context()).Model(&models.Attachment{}).Where("ticket_id = ?", ticketID).Count(&existing).Error; err != nil {
		slog.Error("count attachments", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if existing >= maxFilesPerTicket {
		writeError(w, http.StatusConflict, "file.limit_reached")
		return
	}

	// Multipart overhead on top of the largest allowed file.
	r.Body = http.MaxBytesReader(w, r.Body, maxVideoBytes+(1<<20))
	part, err := filePart(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "file.missing")
		return
	}
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		writeError(w, http.StatusBadRequest, "file.missing")
		return
	}
	head = head[:n]
	mediaType, _ := sniffMedia(head)
	if mediaType == "" {
		writeError(w, http.StatusUnsupportedMediaType, "file.unsupported_type")
		return
	}
	limit := int64(maxImageBytes)
	if mediaType == "video" {
		limit = maxVideoBytes
	}

	dir := filepath.Join(s.UploadDir, strconv.FormatInt(ticketID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Error("upload dir", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	name := make([]byte, 16)
	if _, err := rand.Read(name); err != nil {
		slog.Error("upload name", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	relPath := filepath.Join(strconv.FormatInt(ticketID, 10), hex.EncodeToString(name))
	fullPath := filepath.Join(s.UploadDir, relPath)
	size, err := saveLimited(fullPath, io.MultiReader(bytes.NewReader(head), part), limit)
	if err != nil {
		_ = os.Remove(fullPath)
		var tooBig *http.MaxBytesError
		if errors.Is(err, errTooLarge) || errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "file.too_large")
			return
		}
		slog.Error("save upload", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	a := models.Attachment{TicketID: ticketID, FilePath: filepath.ToSlash(relPath), MediaType: mediaType, SizeBytes: size}
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// Lock the ticket row so two parallel uploads cannot both pass the count check,
		// and re-check the status in case staff closed the ticket during the upload.
		var locked models.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").First(&locked, ticketID).Error; err != nil {
			return err
		}
		if locked.Status == models.StatusClosed {
			return errTicketClosed
		}
		var count int64
		if err := tx.Model(&models.Attachment{}).Where("ticket_id = ?", ticketID).Count(&count).Error; err != nil {
			return err
		}
		if count >= maxFilesPerTicket {
			return errLimitReached
		}
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		return audit.Record(tx, audit.Guest, "attachment.added", audit.Change{
			TicketID: &ticketID, Target: mediaType, To: strconv.FormatInt(size, 10), IP: s.clientIP(r),
		})
	})
	if err != nil {
		_ = os.Remove(fullPath)
		if errors.Is(err, errLimitReached) {
			writeError(w, http.StatusConflict, "file.limit_reached")
			return
		}
		if errors.Is(err, errTicketClosed) {
			writeError(w, http.StatusConflict, "ticket.closed")
			return
		}
		slog.Error("record upload", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	s.publish(r.Context(), ticketID) // FR-P3
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID, "media_type": a.MediaType, "size_bytes": a.SizeBytes})
}

// filePart returns the multipart part named "file", streamed rather than buffered in memory.
func filePart(r *http.Request) (io.Reader, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			return nil, err
		}
		if p.FormName() == "file" {
			return p, nil
		}
	}
}

var errTooLarge = errors.New("file too large")

// saveLimited writes src to path and fails with errTooLarge past limit bytes.
func saveLimited(path string, src io.Reader, limit int64) (int64, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.CopyN(f, src, limit+1)
	closeErr := f.Close()
	if err != nil && !errors.Is(err, io.EOF) { // EOF = file ended before the limit, the normal case
		return n, err
	}
	if closeErr != nil {
		return n, closeErr
	}
	if n > limit {
		return n, errTooLarge
	}
	return n, nil
}
