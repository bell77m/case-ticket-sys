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
