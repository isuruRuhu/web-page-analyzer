package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/isuruRuhu/web-page-analyzer/internal/analyzer"
)

// errTransport is a RoundTripper that fails every request with err.
type errTransport struct {
	err error
}

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// logEntry holds the fields of a JSON log line that the tests check.
type logEntry struct {
	Level      string `json:"level"`
	Msg        string `json:"msg"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMS *int64 `json:"duration_ms"`
	URL        string `json:"url"`
	Err        string `json:"err"`
}

// newTestHandler returns the handler with an analyzer that uses client, the
// buffer it logs to and the registry that holds its metrics.
func newTestHandler(client *http.Client) (http.Handler, *bytes.Buffer, *prometheus.Registry) {
	logs := new(bytes.Buffer)
	reg := prometheus.NewRegistry()
	return NewHandler(slog.New(slog.NewJSONHandler(logs, nil)), analyzer.New(client), reg), logs, reg
}

// decodeLogs parses the JSON log lines in logs, keyed by message.
func decodeLogs(t *testing.T, logs *bytes.Buffer) map[string]logEntry {
	t.Helper()
	entries := make(map[string]logEntry)
	for line := range bytes.Lines(logs.Bytes()) {
		var e logEntry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("decode log entry %q: %v", line, err)
		}
		entries[e.Msg] = e
	}
	return entries
}

// checkRequestLog checks the entry written by the request logging middleware.
func checkRequestLog(t *testing.T, entries map[string]logEntry, method, path string, status int) {
	t.Helper()
	entry, ok := entries["request"]
	if !ok {
		t.Fatal("no request log entry")
	}
	if entry.Method != method {
		t.Errorf("log method = %q, want %q", entry.Method, method)
	}
	if entry.Path != path {
		t.Errorf("log path = %q, want %q", entry.Path, path)
	}
	if entry.Status != status {
		t.Errorf("log status = %d, want %d", entry.Status, status)
	}
	if entry.DurationMS == nil {
		t.Error("log entry has no duration_ms")
	}
}

// checkRows checks that body contains rows in the given order.
func checkRows(t *testing.T, body string, rows []string) {
	t.Helper()
	rest := body
	for _, want := range rows {
		i := strings.Index(rest, want)
		if i < 0 {
			t.Errorf("body does not contain %q after the rows before it", want)
			continue
		}
		rest = rest[i+len(want):]
	}
}

// htmlPage returns a handler that serves page as HTML.
func htmlPage(page string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok\n"},
		{name: "form", method: http.MethodGet, path: "/", wantStatus: http.StatusOK},
		{name: "root matches only exactly", method: http.MethodGet, path: "/foo", wantStatus: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "wrong method for form", method: http.MethodPost, path: "/", wantStatus: http.StatusMethodNotAllowed},
		{name: "wrong method for analyze", method: http.MethodPost, path: "/analyze", wantStatus: http.StatusMethodNotAllowed},
		{name: "no metrics", method: http.MethodGet, path: "/metrics", wantStatus: http.StatusNotFound},
		{name: "no pprof", method: http.MethodGet, path: "/debug/pprof/", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, logs, _ := newTestHandler(&http.Client{})

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			checkRequestLog(t, decodeLogs(t, logs), tt.method, tt.path, tt.wantStatus)
		})
	}
}

func TestForm(t *testing.T) {
	h, _, _ := newTestHandler(&http.Client{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<form action="/analyze" method="get">`,
		`<label for="url">`,
		`<input type="url" id="url" name="url" value="" placeholder="https://example.com" required>`,
		`<button type="submit">Analyze</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	for _, unwanted := range []string{`class="error"`, "<h2>Results</h2>"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("body contains %q", unwanted)
		}
	}
}

func TestAnalyze(t *testing.T) {
	// The external link is plain http because upstream.Client() sends requests
	// for http://example.com to upstream, so checking it needs no internet.
	const loginPage = `<!DOCTYPE html>
<html>
<head><title>Sign in | Example</title></head>
<body>
<h1>Example</h1>
<h2>Account</h2>
<h2>Help</h2>
<form action="/login" method="post">
<input name="user">
<input type="password" name="pass">
</form>
<a href="/forgot">Forgot password?</a>
<a href="/signup">Sign up</a>
<a href="http://example.com/help">Help</a>
</body>
</html>`

	// No handler for /forgot, so that link is inaccessible.
	mux := http.NewServeMux()
	mux.HandleFunc("/login", htmlPage(loginPage))
	mux.HandleFunc("/signup", htmlPage("<title>Sign up</title>"))
	mux.HandleFunc("/help", htmlPage("<title>Help</title>"))
	mux.HandleFunc("/plain", htmlPage("<h3>Only a heading</h3>"))
	mux.Handle("/old", http.RedirectHandler("/plain", http.StatusMovedPermanently))
	upstream := httptest.NewServer(mux)
	defer upstream.Close()

	tests := []struct {
		name      string
		path      string   // on the upstream server
		wantFinal string   // path shown as the analyzed URL
		wantRows  []string // in the order they must appear
	}{
		{
			name:      "login page",
			path:      "/login",
			wantFinal: "/login",
			wantRows: []string{
				`<tr><th scope="row">HTML version</th><td>HTML5</td></tr>`,
				`<tr><th scope="row">Page title</th><td>Sign in | Example</td></tr>`,
				`<tr><th scope="row">h1 headings</th><td>1</td></tr>`,
				`<tr><th scope="row">h2 headings</th><td>2</td></tr>`,
				`<tr><th scope="row">h3 headings</th><td>0</td></tr>`,
				`<tr><th scope="row">h4 headings</th><td>0</td></tr>`,
				`<tr><th scope="row">h5 headings</th><td>0</td></tr>`,
				`<tr><th scope="row">h6 headings</th><td>0</td></tr>`,
				`<tr><th scope="row">Internal links</th><td>2</td></tr>`,
				`<tr><th scope="row">External links</th><td>1</td></tr>`,
				`<tr><th scope="row">Inaccessible links</th><td>1</td></tr>`,
				`<tr><th scope="row">Login form</th><td>Yes</td></tr>`,
			},
		},
		{
			name:      "no doctype, title or login form",
			path:      "/plain",
			wantFinal: "/plain",
			wantRows: []string{
				`<tr><th scope="row">HTML version</th><td>Unknown (no doctype)</td></tr>`,
				`<tr><th scope="row">Page title</th><td><span class="none">(no title)</span></td></tr>`,
				`<tr><th scope="row">h3 headings</th><td>1</td></tr>`,
				`<tr><th scope="row">Internal links</th><td>0</td></tr>`,
				`<tr><th scope="row">External links</th><td>0</td></tr>`,
				`<tr><th scope="row">Inaccessible links</th><td>0</td></tr>`,
				`<tr><th scope="row">Login form</th><td>No</td></tr>`,
			},
		},
		{
			name:      "redirect",
			path:      "/old",
			wantFinal: "/plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, logs, reg := newTestHandler(upstream.Client())
			submitted := upstream.URL + tt.path
			path := "/analyze?url=" + url.QueryEscape(submitted)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			body := rec.Body.String()
			for _, want := range []string{
				`name="url" value="` + submitted + `"`,
				`<tr><th scope="row">Analyzed URL</th><td>` + upstream.URL + tt.wantFinal + `</td></tr>`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q", want)
				}
			}
			checkRows(t, body, tt.wantRows)
			if strings.Contains(body, `class="error"`) {
				t.Error("body contains an error")
			}
			if strings.Contains(body, "Unchecked links") {
				t.Error("body contains an unchecked links row")
			}
			checkRequestLog(t, decodeLogs(t, logs), http.MethodGet, "/analyze", http.StatusOK)
			checkAnalysisMetrics(t, reg, "ok")
		})
	}
}

func TestAnalyzeErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/missing", http.NotFoundHandler())
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/odd", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(599)
	})
	mux.HandleFunc("/data.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
	})
	upstream := httptest.NewServer(mux)
	defer upstream.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	failing := func(err error) *http.Client {
		return &http.Client{Transport: errTransport{err: err}}
	}
	const (
		enterURL     = " Enter a full URL such as https://example.com."
		statusMsg    = "The page responded with this error status, so it could not be analyzed."
		unreachedMsg = "The page could not be reached: "
	)

	tests := []struct {
		name        string
		client      *http.Client // upstream.Client() if nil
		path        string       // "/analyze?url=" plus the escaped target if empty
		target      string
		wantStatus  int
		wantTitle   string
		wantMessage string
		wantOutcome string
	}{
		{
			name:        "missing url parameter",
			path:        "/analyze",
			wantStatus:  http.StatusBadRequest,
			wantTitle:   "400 Bad Request",
			wantMessage: "The URL is empty." + enterURL,
			wantOutcome: "invalid_url",
		},
		{
			name:        "empty url parameter",
			path:        "/analyze?url=",
			wantStatus:  http.StatusBadRequest,
			wantTitle:   "400 Bad Request",
			wantMessage: "The URL is empty." + enterURL,
			wantOutcome: "invalid_url",
		},
		{
			name:        "missing scheme",
			target:      "example.com",
			wantStatus:  http.StatusBadRequest,
			wantTitle:   "400 Bad Request",
			wantMessage: "The URL has no http:// or https:// prefix." + enterURL,
			wantOutcome: "invalid_url",
		},
		{
			name:        "unsupported scheme",
			target:      "ftp://example.com/file",
			wantStatus:  http.StatusBadRequest,
			wantTitle:   "400 Bad Request",
			wantMessage: `The URL must use http or https, not "ftp".` + enterURL,
			wantOutcome: "invalid_url",
		},
		{
			name:        "malformed URL",
			target:      "http://exa mple.com",
			wantStatus:  http.StatusBadRequest,
			wantTitle:   "400 Bad Request",
			wantMessage: `Invalid character " " in host name.` + enterURL,
			wantOutcome: "invalid_url",
		},
		{
			name:        "page not found",
			target:      upstream.URL + "/missing",
			wantStatus:  http.StatusBadGateway,
			wantTitle:   "404 Not Found",
			wantMessage: statusMsg,
			wantOutcome: "bad_status",
		},
		{
			name:        "page server error",
			target:      upstream.URL + "/broken",
			wantStatus:  http.StatusBadGateway,
			wantTitle:   "500 Internal Server Error",
			wantMessage: statusMsg,
			wantOutcome: "bad_status",
		},
		{
			name:        "status without standard text",
			target:      upstream.URL + "/odd",
			wantStatus:  http.StatusBadGateway,
			wantTitle:   "599",
			wantMessage: statusMsg,
			wantOutcome: "bad_status",
		},
		{
			name:        "timeout",
			client:      failing(os.ErrDeadlineExceeded),
			target:      "http://example.com",
			wantStatus:  http.StatusGatewayTimeout,
			wantTitle:   "504 Gateway Timeout",
			wantMessage: unreachedMsg + "request timed out.",
			wantOutcome: "timeout",
		},
		{
			name:        "host not found",
			client:      failing(&net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}),
			target:      "http://example.invalid",
			wantStatus:  http.StatusBadGateway,
			wantTitle:   "502 Bad Gateway",
			wantMessage: unreachedMsg + "host name could not be resolved.",
			wantOutcome: "unreachable",
		},
		{
			name:        "connection refused",
			target:      closed.URL,
			wantStatus:  http.StatusBadGateway,
			wantTitle:   "502 Bad Gateway",
			wantMessage: unreachedMsg + "connection refused.",
			wantOutcome: "unreachable",
		},
		{
			name:        "private address",
			client:      analyzer.NewClient(time.Second, false),
			target:      upstream.URL,
			wantStatus:  http.StatusForbidden,
			wantTitle:   "403 Forbidden",
			wantMessage: "The URL points to a private or local address, which is not allowed.",
			wantOutcome: "blocked",
		},
		{
			name:        "not HTML",
			target:      upstream.URL + "/data.json",
			wantStatus:  http.StatusUnprocessableEntity,
			wantTitle:   "422 Unprocessable Entity",
			wantMessage: "The URL does not point to an HTML page, so it could not be analyzed.",
			wantOutcome: "not_html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client
			if client == nil {
				client = upstream.Client()
			}
			path := tt.path
			if path == "" {
				path = "/analyze?url=" + url.QueryEscape(tt.target)
			}
			h, logs, reg := newTestHandler(client)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			body := rec.Body.String()
			for _, want := range []string{
				"<h2>" + tt.wantTitle + "</h2>",
				"<p>" + template.HTMLEscapeString(tt.wantMessage) + "</p>",
				`name="url" value="` + template.HTMLEscapeString(tt.target) + `"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q", want)
				}
			}
			if strings.Contains(body, "<h2>Results</h2>") {
				t.Error("body contains results")
			}

			entries := decodeLogs(t, logs)
			failed, ok := entries["analysis failed"]
			if !ok {
				t.Fatal("no analysis failed log entry")
			}
			if failed.Level != "WARN" {
				t.Errorf("log level = %q, want WARN", failed.Level)
			}
			if failed.URL != tt.target {
				t.Errorf("log url = %q, want %q", failed.URL, tt.target)
			}
			if failed.Err == "" {
				t.Error("log entry has no err")
			}
			checkRequestLog(t, entries, http.MethodGet, "/analyze", tt.wantStatus)
			checkAnalysisMetrics(t, reg, tt.wantOutcome)
		})
	}
}

func TestAnalyzeEscapesUserText(t *testing.T) {
	const script = "<script>alert(1)</script>"
	upstream := httptest.NewServer(htmlPage("<title>" + script + "</title>"))
	defer upstream.Close()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "page title",
			target: upstream.URL,
			want:   `<td>&lt;script&gt;alert(1)&lt;/script&gt;</td>`,
		},
		{
			name:   "submitted URL",
			target: `"><script>alert(1)</script>`,
			want:   `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, _ := newTestHandler(upstream.Client())

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyze?url="+url.QueryEscape(tt.target), nil))

			body := rec.Body.String()
			if !strings.Contains(body, tt.want) {
				t.Errorf("body does not contain %q", tt.want)
			}
			if strings.Contains(body, script) {
				t.Errorf("body contains unescaped %q", script)
			}
		})
	}
}

func TestAnalyzeClientGone(t *testing.T) {
	tests := []struct {
		name        string
		cancelFirst bool // cancel before the request rather than when the page's link is checked
	}{
		// Analyze fails, since the page cannot be fetched.
		{name: "before the page is fetched", cancelFirst: true},
		// The page was fetched, so Analyze returns a result without an error.
		{name: "while links are checked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			mux := http.NewServeMux()
			mux.HandleFunc("/{$}", htmlPage(`<a href="/link">Link</a>`))
			mux.HandleFunc("/link", func(w http.ResponseWriter, r *http.Request) { cancel() })
			upstream := httptest.NewServer(mux)
			defer upstream.Close()
			if tt.cancelFirst {
				cancel()
			}
			h, logs, reg := newTestHandler(upstream.Client())

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/analyze?url="+url.QueryEscape(upstream.URL), nil)
			h.ServeHTTP(rec, req)

			if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", rec.Body.String())
			}
			if _, ok := decodeLogs(t, logs)["analysis failed"]; ok {
				t.Error("logged a failed analysis for a client that went away")
			}
			checkAnalysisMetrics(t, reg, "canceled")
		})
	}
}

func TestRenderUncheckedLinks(t *testing.T) {
	const (
		inaccessibleRow = `<tr><th scope="row">Inaccessible links</th><td>2</td></tr>`
		uncheckedRow    = `<tr><th scope="row">Unchecked links</th><td>3 <span class="none">(not checked, the time or link limit was reached)</span></td></tr>`
		loginRow        = `<tr><th scope="row">Login form</th><td>No</td></tr>`
	)

	tests := []struct {
		name      string
		unchecked int
		wantRows  []string // in the order they must appear
	}{
		{name: "all links checked", wantRows: []string{inaccessibleRow, loginRow}},
		{name: "some links unchecked", unchecked: 3, wantRows: []string{inaccessibleRow, uncheckedRow, loginRow}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{logger: slog.New(slog.DiscardHandler), page: pageTemplate}
			res := &analyzer.Result{InaccessibleLinks: 2, UncheckedLinks: tt.unchecked}

			rec := httptest.NewRecorder()
			s.render(rec, http.StatusOK, pageData{Result: res})

			body := rec.Body.String()
			checkRows(t, body, tt.wantRows)
			if tt.unchecked == 0 && strings.Contains(body, "Unchecked links") {
				t.Error("body contains an unchecked links row")
			}
		})
	}
}

func TestRenderErrorUnexpected(t *testing.T) {
	var logs bytes.Buffer
	s := &server{logger: slog.New(slog.NewJSONHandler(&logs, nil)), page: pageTemplate}
	const detail = "internal detail the user must not see"

	rec := httptest.NewRecorder()
	outcome := s.renderError(rec, httptest.NewRequest(http.MethodGet, "/analyze", nil), "https://example.com", errors.New(detail))

	if outcome != "error" {
		t.Errorf("outcome = %q, want error", outcome)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<h2>500 Internal Server Error</h2>",
		"<p>Something went wrong while analyzing the page. Please try again later.</p>",
		`name="url" value="https://example.com"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
	if strings.Contains(body, detail) {
		t.Errorf("body contains the error detail %q", detail)
	}

	failed, ok := decodeLogs(t, &logs)["analysis failed"]
	if !ok {
		t.Fatal("no analysis failed log entry")
	}
	if failed.Level != "ERROR" {
		t.Errorf("log level = %q, want ERROR", failed.Level)
	}
	if failed.Err != detail {
		t.Errorf("log err = %q, want %q", failed.Err, detail)
	}
}

func TestRenderTemplateError(t *testing.T) {
	var logs bytes.Buffer
	broken := template.Must(template.New("page").Parse("<p>half a page {{.Missing}}</p>"))
	s := &server{logger: slog.New(slog.NewJSONHandler(&logs, nil)), page: broken}

	rec := httptest.NewRecorder()
	s.handleForm(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if want := "Internal Server Error\n"; rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
	entry, ok := decodeLogs(t, &logs)["render page"]
	if !ok {
		t.Fatal("no render page log entry")
	}
	if entry.Level != "ERROR" {
		t.Errorf("log level = %q, want ERROR", entry.Level)
	}
}
