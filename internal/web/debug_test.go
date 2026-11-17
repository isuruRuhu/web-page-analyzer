package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewDebugHandler(t *testing.T) {
	// Vector metrics only show up once they have a series, so make a request.
	app, _, reg := newTestHandler(&http.Client{})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h := NewDebugHandler(reg)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // part of the body, if set
	}{
		{
			name:       "metrics",
			path:       "/metrics",
			wantStatus: http.StatusOK,
			wantBody:   `http_requests_total{method="GET",route="GET /healthz",status="200"} 1`,
		},
		{name: "pprof index", path: "/debug/pprof/", wantStatus: http.StatusOK, wantBody: "goroutine"},
		{name: "pprof cmdline", path: "/debug/pprof/cmdline", wantStatus: http.StatusOK},
		{name: "pprof symbol", path: "/debug/pprof/symbol", wantStatus: http.StatusOK, wantBody: "num_symbols"},
		{name: "pprof trace", path: "/debug/pprof/trace?seconds=0.01", wantStatus: http.StatusOK},
		{name: "no app routes", path: "/healthz", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("body does not contain %q", tt.wantBody)
			}
		})
	}
}
