// Package web serves the SvelteKit build embedded in the binary.
// `make build` and the Dockerfile copy frontend/build into web/dist before `go build`.
package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded frontend.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a compile-time embed path; this cannot fail at runtime.
	}
	return spaHandler(sub)
}

// spaHandler serves files from fsys and falls back to index.html for client-side routes.
// Paths under /_app/ are build assets, so a missing one is a real 404.
// Files under /_app/immutable/ have a content hash in their name, so browsers may cache them for good.
func spaHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/") // rooted, so ".." cannot climb out
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(fsys, name); err != nil {
			if strings.HasPrefix(name, "_app/") {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		if strings.HasPrefix(name, "_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		serveFile(w, r, fsys, name)
	})
}

// serveFile sends the .br or .gz copy of name that adapter-static's precompress wrote, when the client accepts it.
// ponytail: substring match on Accept-Encoding ignores q=0; browsers never send it for br or gzip.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	accept := r.Header.Get("Accept-Encoding")
	for _, enc := range [...]struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if !strings.Contains(accept, enc.token) {
			continue
		}
		if _, err := fs.Stat(fsys, name+enc.ext); err != nil {
			continue
		}
		h := w.Header()
		h.Set("Content-Encoding", enc.token)
		h.Set("Content-Type", mime.TypeByExtension(path.Ext(name))) // precompress only covers types mime knows
		h.Set("Vary", "Accept-Encoding")
		http.ServeFileFS(w, r, fsys, name+enc.ext)
		return
	}
	http.ServeFileFS(w, r, fsys, name)
}
