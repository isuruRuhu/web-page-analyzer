package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// metrics holds the Prometheus metrics recorded by the handlers.
type metrics struct {
	requests         *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	analyses         *prometheus.CounterVec
	analysisDuration prometheus.Histogram
}

// newMetrics creates the metrics and registers them with reg.
func newMetrics(reg prometheus.Registerer) *metrics {
	// An analysis can take up to 45 seconds, so the buckets go past that.
	buckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 45, 60}
	m := &metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests handled.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Time taken to handle HTTP requests.",
			Buckets: buckets,
		}, []string{"route"}),
		analyses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "page_analyses_total",
			Help: "Page analyses by outcome.",
		}, []string{"outcome"}),
		analysisDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "page_analysis_duration_seconds",
			Help:    "Time taken to analyze a page.",
			Buckets: buckets,
		}),
	}
	reg.MustRegister(m.requests, m.requestDuration, m.analyses, m.analysisDuration)
	return m
}

func (m *metrics) observeRequest(r *http.Request, status int, d time.Duration) {
	route := routeLabel(r)
	m.requests.WithLabelValues(methodLabel(r.Method), route, strconv.Itoa(status)).Inc()
	m.requestDuration.WithLabelValues(route).Observe(d.Seconds())
}

func (m *metrics) observeAnalysis(outcome string, d time.Duration) {
	m.analyses.WithLabelValues(outcome).Inc()
	m.analysisDuration.Observe(d.Seconds())
}

// routeLabel returns the mux pattern that matched r. Requests that matched no
// route share one label, so that arbitrary paths cannot create new series.
func routeLabel(r *http.Request) string {
	if r.Pattern == "" {
		return "unmatched"
	}
	return r.Pattern
}

// methodLabel returns method if it is a standard one. Clients can send any
// token as the method, so the rest share one label.
func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}
	return "other"
}
