// Package web contains the HTTP handlers and middleware for the analyzer.
package web

import (
	"fmt"
	"log/slog"
	"net/http"
)

// NewHandler returns the application's routes wrapped in request logging.
func NewHandler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	return logRequests(logger, mux)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}
