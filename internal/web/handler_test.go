package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok\n"},
		{name: "unknown path", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}

			var entry struct {
				Msg        string `json:"msg"`
				Method     string `json:"method"`
				Path       string `json:"path"`
				Status     int    `json:"status"`
				DurationMS *int64 `json:"duration_ms"`
			}
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatalf("decode log entry %q: %v", logs.String(), err)
			}
			if entry.Msg != "request" {
				t.Errorf("log msg = %q, want %q", entry.Msg, "request")
			}
			if entry.Method != tt.method {
				t.Errorf("log method = %q, want %q", entry.Method, tt.method)
			}
			if entry.Path != tt.path {
				t.Errorf("log path = %q, want %q", entry.Path, tt.path)
			}
			if entry.Status != tt.wantStatus {
				t.Errorf("log status = %d, want %d", entry.Status, tt.wantStatus)
			}
			if entry.DurationMS == nil {
				t.Error("log entry has no duration_ms")
			}
		})
	}
}
