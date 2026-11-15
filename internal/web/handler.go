// Package web contains the HTTP handlers and middleware for the analyzer.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/isuruRuhu/web-page-analyzer/internal/analyzer"
)

//go:embed templates/page.html
var templateFS embed.FS

// pageTemplate is parsed once at startup; a broken template stops the program.
var pageTemplate = template.Must(template.ParseFS(templateFS, "templates/page.html"))

// server holds what the page handlers need.
type server struct {
	logger   *slog.Logger
	analyzer *analyzer.Analyzer
	page     *template.Template
}

// pageData is what the page template renders. Result and Problem are nil
// until the user submits a URL.
type pageData struct {
	URL     string // as entered by the user
	Result  *analyzer.Result
	Problem *problem
}

// NewHandler returns the application's routes wrapped in request logging.
func NewHandler(logger *slog.Logger, a *analyzer.Analyzer) http.Handler {
	s := &server{logger: logger, analyzer: a, page: pageTemplate}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleForm)
	mux.HandleFunc("GET /analyze", s.handleAnalyze)
	mux.HandleFunc("GET /healthz", handleHealthz)
	return logRequests(logger, mux)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "ok")
}

func (s *server) handleForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, pageData{})
}

func (s *server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	res, err := s.analyzer.Analyze(r.Context(), rawURL)
	if err != nil {
		// The client went away, so there is nobody to show the error to.
		if r.Context().Err() != nil {
			return
		}
		s.renderError(w, r, rawURL, err)
		return
	}
	s.render(w, http.StatusOK, pageData{URL: rawURL, Result: res})
}

// renderError logs a failed analysis and shows the error below the form.
func (s *server) renderError(w http.ResponseWriter, r *http.Request, rawURL string, err error) {
	status, p := describeError(err)
	level := slog.LevelWarn
	if status == http.StatusInternalServerError {
		level = slog.LevelError
	}
	s.logger.Log(r.Context(), level, "analysis failed", "url", rawURL, "err", err)
	s.render(w, status, pageData{URL: rawURL, Problem: p})
}

// render writes the page with the given status. It renders into a buffer
// first so a template error becomes a plain 500 rather than half a page.
func (s *server) render(w http.ResponseWriter, status int, data pageData) {
	var buf bytes.Buffer
	if err := s.page.Execute(&buf, data); err != nil {
		s.logger.Error("render page", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}
