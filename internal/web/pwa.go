package web

import (
	"net/http"
)

// serveStaticFile writes an embedded static file with an explicit content type.
func serveStaticFile(w http.ResponseWriter, name, contentType string) {
	sub, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(sub)
}

// handleManifest serves the PWA web manifest at the root scope.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	serveStaticFile(w, "manifest.webmanifest", "application/manifest+json")
}

// handleServiceWorker serves the service worker at root scope so it controls
// the whole origin.
func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Service-Worker-Allowed", "/")
	serveStaticFile(w, "sw.js", "application/javascript")
}

// handleOffline serves the offline fallback page.
func (s *Server) handleOffline(w http.ResponseWriter, r *http.Request) {
	serveStaticFile(w, "offline.html", "text/html; charset=utf-8")
}
