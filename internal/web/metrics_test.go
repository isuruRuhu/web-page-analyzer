package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// histogramCount returns how many observations the histogram name in reg
// holds, over all its label values.
func histogramCount(t *testing.T, reg prometheus.Gatherer, name string) uint64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var n uint64
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			n += m.GetHistogram().GetSampleCount()
		}
	}
	return n
}

// checkAnalysisMetrics checks that reg holds exactly one analysis, with outcome.
func checkAnalysisMetrics(t *testing.T, reg prometheus.Gatherer, outcome string) {
	t.Helper()
	want := `
# HELP page_analyses_total Page analyses by outcome.
# TYPE page_analyses_total counter
page_analyses_total{outcome="` + outcome + `"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "page_analyses_total"); err != nil {
		t.Error(err)
	}
	if n := histogramCount(t, reg, "page_analysis_duration_seconds"); n != 1 {
		t.Errorf("page_analysis_duration_seconds count = %d, want 1", n)
	}
}

func TestRequestMetrics(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   string // the only http_requests_total series
	}{
		{
			name:   "matched route",
			method: http.MethodGet,
			path:   "/healthz",
			want:   `http_requests_total{method="GET",route="GET /healthz",status="200"} 1`,
		},
		{
			name:   "query string",
			method: http.MethodGet,
			path:   "/analyze?url=notaurl",
			want:   `http_requests_total{method="GET",route="GET /analyze",status="400"} 1`,
		},
		{
			name:   "unmatched path",
			method: http.MethodGet,
			path:   "/no/such/page",
			want:   `http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		},
		{
			name:   "wrong method",
			method: http.MethodPost,
			path:   "/healthz",
			want:   `http_requests_total{method="POST",route="unmatched",status="405"} 1`,
		},
		{
			name:   "nonstandard method",
			method: "BREW",
			path:   "/healthz",
			want:   `http_requests_total{method="other",route="unmatched",status="405"} 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, reg := newTestHandler(&http.Client{})

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tt.method, tt.path, nil))

			want := `
# HELP http_requests_total HTTP requests handled.
# TYPE http_requests_total counter
` + tt.want + "\n"
			if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "http_requests_total"); err != nil {
				t.Error(err)
			}
			if n := histogramCount(t, reg, "http_request_duration_seconds"); n != 1 {
				t.Errorf("http_request_duration_seconds count = %d, want 1", n)
			}
		})
	}
}
