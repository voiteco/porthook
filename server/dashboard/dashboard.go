// SPDX-License-Identifier: AGPL-3.0-only

package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static/*
var embeddedAssets embed.FS

// Handler serves the self-hosted admin dashboard under /dashboard/.
func Handler() http.Handler {
	assets, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		switch {
		case r.URL.Path == "/dashboard":
			http.Redirect(w, r, "/dashboard/", http.StatusMovedPermanently)
		case r.URL.Path == "/dashboard/":
			serveIndex(w, r)
		case strings.HasPrefix(r.URL.Path, "/dashboard/"):
			http.StripPrefix("/dashboard/", fileServer).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

func serveIndex(w http.ResponseWriter, _ *http.Request) {
	data, err := embeddedAssets.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "dashboard index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self' http: https:; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
}
