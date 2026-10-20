package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js style.css favicon.svg
var files embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		name := r.URL.Path
		if name != "/" && name != "/index.html" && name != "/app.js" && name != "/style.css" && name != "/favicon.svg" {
			http.NotFound(w, r)
			return
		}
		sub, _ := fs.Sub(files, ".")
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	})
}
