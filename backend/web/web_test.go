package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandler(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":          {Data: []byte("<html>app</html>")},
		"_app/immutable/a.js": {Data: []byte("js")},
	}
	h := spaHandler(fsys)

	tests := []struct {
		path     string
		wantCode int
		wantBody string
	}{
		{"/", 200, "<html>app</html>"},
		{"/_app/immutable/a.js", 200, "js"},
		{"/track/abc", 200, "<html>app</html>"}, // client-side route falls back to index.html
		{"/_app/immutable/missing.js", 404, ""}, // missing assets stay 404, not index.html
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			body, _ := io.ReadAll(rec.Body)
			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantBody != "" && string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// The build is precompressed (adapter-static precompress) and hashed assets never change, so the handler sends the
// compressed copy the client accepts and lets browsers cache /_app/immutable/ for good.
func TestSPAHandler_CompressionAndCaching(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":             {Data: []byte("<html>app</html>")},
		"index.html.gz":          {Data: []byte("gz-html")},
		"_app/immutable/a.js":    {Data: []byte("js")},
		"_app/immutable/a.js.br": {Data: []byte("br-js")},
		"_app/immutable/a.js.gz": {Data: []byte("gz-js")},
		"favicon.svg":            {Data: []byte("<svg/>")},
	}
	h := spaHandler(fsys)

	tests := []struct {
		path, acceptEncoding             string
		wantBody, wantEncoding, wantType string
		wantCacheControl                 string
	}{
		{"/_app/immutable/a.js", "gzip, deflate, br, zstd", "br-js", "br", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/_app/immutable/a.js", "gzip", "gz-js", "gzip", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/_app/immutable/a.js", "", "js", "", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"},
		{"/track/abc", "gzip, br", "gz-html", "gzip", "text/html; charset=utf-8", ""}, // index.html has no .br here
		{"/favicon.svg", "gzip, br", "<svg/>", "", "image/svg+xml", ""},               // no compressed copy
	}
	for _, tt := range tests {
		t.Run(tt.path+" "+tt.acceptEncoding, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			body, _ := io.ReadAll(rec.Body)
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
			if got := rec.Header().Get("Content-Encoding"); got != tt.wantEncoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tt.wantEncoding)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCacheControl {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCacheControl)
			}
			if tt.wantEncoding != "" && rec.Header().Get("Vary") != "Accept-Encoding" {
				t.Errorf("Vary = %q, want Accept-Encoding", rec.Header().Get("Vary"))
			}
		})
	}
}
