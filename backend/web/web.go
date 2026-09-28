// Package web serves the SvelteKit build embedded in the binary.
// `make build` and the Dockerfile copy frontend/build into web/dist before `go build`.
package web

import (
	"embed"
	"io/fs"
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
func spaHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/") // rooted, so ".." cannot climb out
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(fsys, name); err != nil && !strings.HasPrefix(name, "_app/") {
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		files.ServeHTTP(w, r)
	})
}
