//go:build embedweb

package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// webdist is a copy of web/dist placed here by deploy/deploy.sh before `go build -tags embedweb`.
//
//go:embed all:webdist
var webdist embed.FS

// webHandler serves the built app: long cache for hashed assets, no-cache for the rest, index.html fallback.
func webHandler() http.Handler {
	root, _ := fs.Sub(webdist, "webdist")
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p != "" {
			if _, err := fs.Stat(root, p); err != nil {
				r.URL.Path = "/" // unknown path: let the app's router handle it
			}
		}
		files.ServeHTTP(w, r)
	})
}
