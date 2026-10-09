package api

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// clamdTimeout covers sending a 100 MB video to clamd and its scan.
const clamdTimeout = 2 * time.Minute

// scanFile sends a stored upload to clamd (CLAMD_ADDR) and returns the matched signature, or "" when the file is
// clean (NFR-5). The YARA rules live in deploy/base/clamav. Any failure is an error: the caller stores nothing.
func (s *Server) scanFile(ctx context.Context, path string) (string, error) {
	if s.ClamdAddr == "" {
		return "", errors.New("clamd: CLAMD_ADDR not set")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() // read-only file: a close error loses nothing
	ctx, cancel := context.WithTimeout(ctx, clamdTimeout)
	defer cancel()
	return clamdScan(ctx, s.ClamdAddr, f)
}

// clamdScan streams src to clamd with INSTREAM: "zINSTREAM\0", chunks of <4-byte big-endian length><data>, then a
// zero length. clamd answers "stream: OK" or "stream: <signature> FOUND"; anything else is an error.
func clamdScan(ctx context.Context, addr string, src io.Reader) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("clamd: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return "", fmt.Errorf("clamd: %w", err)
	}
	buf := make([]byte, 4+64<<10)
	for {
		n, rerr := src.Read(buf[4:])
		if n > 0 {
			binary.BigEndian.PutUint32(buf, uint32(n))
			if _, err := conn.Write(buf[:4+n]); err != nil {
				return "", fmt.Errorf("clamd: %w", err)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("read upload: %w", rerr)
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return "", fmt.Errorf("clamd: %w", err)
	}
	// The reply must be complete (NUL-terminated); a cut-off one is an error, never "clean".
	reply, err := bufio.NewReader(io.LimitReader(conn, 1024)).ReadString(0)
	if err != nil {
		return "", fmt.Errorf("clamd reply: %w", err)
	}
	reply = strings.TrimSuffix(reply, "\x00")
	if reply == "stream: OK" {
		return "", nil
	}
	if sig, ok := strings.CutPrefix(reply, "stream: "); ok {
		if sig, ok = strings.CutSuffix(sig, " FOUND"); ok && sig != "" {
			return sig, nil
		}
	}
	return "", fmt.Errorf("clamd: unexpected reply %q", reply)
}
