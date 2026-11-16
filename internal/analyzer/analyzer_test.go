package analyzer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// errTransport is a RoundTripper that fails every request with err.
type errTransport struct {
	err error
}

func (t errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestFetch(t *testing.T) {
	const page = "<!DOCTYPE html><html><head><title>Test</title></head></html>"

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantBody  string
		wantFinal string // path appended to the server URL
	}{
		{
			name: "html",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, page)
			},
			wantBody: page,
		},
		{
			name: "xhtml",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xhtml+xml")
				fmt.Fprint(w, page)
			},
			wantBody: page,
		},
		{
			name: "missing content type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// A nil value stops the server from sniffing one.
				w.Header()["Content-Type"] = nil
				fmt.Fprint(w, "plain text")
			},
			wantBody: "plain text",
		},
		{
			name: "redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/final" {
					http.Redirect(w, r, "/final", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, page)
			},
			wantBody:  page,
			wantFinal: "/final",
		},
		{
			name: "sends user agent",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, r.UserAgent())
			},
			wantBody: userAgent,
		},
		{
			name: "charset from header",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
				fmt.Fprint(w, "<title>Z\xfcrich</title>")
			},
			wantBody: "<title>Zürich</title>",
		},
		{
			name: "charset from meta tag",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<meta charset=\"windows-1252\"><title>Caf\xe9</title>")
			},
			wantBody: `<meta charset="windows-1252"><title>Café</title>`,
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
			},
			wantBody: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			final, body, err := New(ts.Client()).fetch(t.Context(), mustParse(t, ts.URL))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
			if want := ts.URL + tt.wantFinal; final.String() != want {
				t.Errorf("final URL = %q, want %q", final.String(), want)
			}
		})
	}
}

func TestFetchBodyLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(bytes.Repeat([]byte("a"), maxBodySize+1024))
	}))
	defer ts.Close()

	_, body, err := New(ts.Client()).fetch(t.Context(), mustParse(t, ts.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(body) != maxBodySize {
		t.Errorf("body length = %d, want %d", len(body), maxBodySize)
	}
}

func TestFetchStatusError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
	}{
		{name: "not found", status: http.StatusNotFound, contentType: "text/html"},
		{name: "server error", status: http.StatusInternalServerError, contentType: "text/html"},
		{name: "status checked before content type", status: http.StatusNotFound, contentType: "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()

			_, _, err := New(ts.Client()).fetch(t.Context(), mustParse(t, ts.URL))
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *StatusError", err)
			}
			if se.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", se.StatusCode, tt.status)
			}
		})
	}
}

func TestFetchNotHTML(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "json", contentType: "application/json"},
		{name: "image", contentType: "image/png"},
		{name: "malformed", contentType: "text/html; charset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				fmt.Fprint(w, "{}")
			}))
			defer ts.Close()

			_, _, err := New(ts.Client()).fetch(t.Context(), mustParse(t, ts.URL))
			if !errors.Is(err, ErrNotHTML) {
				t.Errorf("err = %v, want ErrNotHTML", err)
			}
		})
	}
}

func TestFetchUnreachable(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer slow.Close()

	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer stalled.Close()

	// charset.NewReader reads the first 1024 bytes on its own, so this one
	// stalls after them to fail in the later read.
	stalledLater := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(bytes.Repeat([]byte("a"), 2048))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer stalledLater.Close()

	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer loop.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	expired, cancel := context.WithDeadline(context.Background(), time.Now())
	defer cancel()

	failing := func(err error) *http.Client {
		return &http.Client{Transport: errTransport{err: err}}
	}
	quick := &http.Client{Timeout: 50 * time.Millisecond}

	tests := []struct {
		name        string
		client      *http.Client
		ctx         context.Context // t.Context() if nil
		url         string
		wantReason  string
		wantTimeout bool
	}{
		{
			name:       "host not found",
			client:     failing(&net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}),
			url:        "http://example.invalid",
			wantReason: "host name could not be resolved",
		},
		{
			name:        "dns timeout",
			client:      failing(&net.DNSError{Err: "i/o timeout", Name: "example.invalid", IsTimeout: true}),
			url:         "http://example.invalid",
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:        "network timeout",
			client:      failing(os.ErrDeadlineExceeded),
			url:         "http://example.com",
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:       "bad certificate",
			client:     failing(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}),
			url:        "https://example.com",
			wantReason: "TLS certificate could not be verified",
		},
		{
			name:       "other error",
			client:     failing(io.ErrUnexpectedEOF),
			url:        "http://example.com",
			wantReason: "request failed",
		},
		{
			name:       "connection refused",
			client:     &http.Client{},
			url:        closed.URL,
			wantReason: "connection refused",
		},
		{
			name:        "client timeout",
			client:      quick,
			url:         slow.URL,
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:        "context deadline",
			client:      &http.Client{},
			ctx:         expired,
			url:         slow.URL,
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:        "timeout reading body",
			client:      quick,
			url:         stalled.URL,
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:        "timeout after first kilobyte",
			client:      quick,
			url:         stalledLater.URL,
			wantReason:  "request timed out",
			wantTimeout: true,
		},
		{
			name:       "redirect loop",
			client:     &http.Client{},
			url:        loop.URL,
			wantReason: "too many redirects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if ctx == nil {
				ctx = t.Context()
			}

			_, _, err := New(tt.client).fetch(ctx, mustParse(t, tt.url))
			var ue *UnreachableError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want *UnreachableError", err)
			}
			if ue.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q (err: %v)", ue.Reason, tt.wantReason, err)
			}
			if ue.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", ue.Timeout, tt.wantTimeout)
			}
		})
	}
}

func TestAnalyze(t *testing.T) {
	// The external link is plain http because ts.Client() sends requests for
	// http://example.com to ts, so checking it needs no internet.
	const page = `<!DOCTYPE html>
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
	noHeadings := map[string]int{"h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0}

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantFinal string // path appended to the server URL
		want      Result // URL is left empty, wantFinal covers it
	}{
		{
			name: "full page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprint(w, page)
			},
			want: Result{
				HTMLVersion:   "HTML5",
				Title:         "Sign in | Example",
				Headings:      map[string]int{"h1": 1, "h2": 2, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
				InternalLinks: 2,
				ExternalLinks: 1,
				HasLoginForm:  true,
			},
		},
		{
			name: "redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/final" {
					http.Redirect(w, r, "/final", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<!DOCTYPE html><title>Final</title>")
			},
			wantFinal: "/final",
			want:      Result{HTMLVersion: "HTML5", Title: "Final", Headings: noHeadings},
		},
		{
			name: "latin-1 page",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
				fmt.Fprint(w, "<!DOCTYPE html><title>Gr\xfc\xdfe aus Z\xfcrich</title>")
			},
			want: Result{HTMLVersion: "HTML5", Title: "Grüße aus Zürich", Headings: noHeadings},
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
			},
			want: Result{HTMLVersion: "Unknown (no doctype)", Headings: noHeadings},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			res, err := New(ts.Client()).Analyze(t.Context(), ts.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if want := ts.URL + tt.wantFinal; res.URL != want {
				t.Errorf("URL = %q, want %q", res.URL, want)
			}
			if res.HTMLVersion != tt.want.HTMLVersion {
				t.Errorf("HTMLVersion = %q, want %q", res.HTMLVersion, tt.want.HTMLVersion)
			}
			if res.Title != tt.want.Title {
				t.Errorf("Title = %q, want %q", res.Title, tt.want.Title)
			}
			if !maps.Equal(res.Headings, tt.want.Headings) {
				t.Errorf("Headings = %v, want %v", res.Headings, tt.want.Headings)
			}
			if res.InternalLinks != tt.want.InternalLinks {
				t.Errorf("InternalLinks = %d, want %d", res.InternalLinks, tt.want.InternalLinks)
			}
			if res.ExternalLinks != tt.want.ExternalLinks {
				t.Errorf("ExternalLinks = %d, want %d", res.ExternalLinks, tt.want.ExternalLinks)
			}
			if res.InaccessibleLinks != tt.want.InaccessibleLinks {
				t.Errorf("InaccessibleLinks = %d, want %d", res.InaccessibleLinks, tt.want.InaccessibleLinks)
			}
			if res.UncheckedLinks != tt.want.UncheckedLinks {
				t.Errorf("UncheckedLinks = %d, want %d", res.UncheckedLinks, tt.want.UncheckedLinks)
			}
			if res.HasLoginForm != tt.want.HasLoginForm {
				t.Errorf("HasLoginForm = %v, want %v", res.HasLoginForm, tt.want.HasLoginForm)
			}
		})
	}
}

func TestAnalyzeLinksAfterRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "old.test" {
			http.Redirect(w, r, "http://new.test/home", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<a href="/about">About</a><a href="http://new.test/blog">Blog</a><a href="http://new.test/shop">Shop</a><a href="http://old.test/">Old site</a>`)
	}))
	defer ts.Close()

	// Connect to ts whatever the host, so old.test and new.test need no DNS.
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, ts.Listener.Addr().String())
		},
	}}

	res, err := New(client).Analyze(t.Context(), "http://old.test/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "http://new.test/home"; res.URL != want {
		t.Errorf("URL = %q, want %q", res.URL, want)
	}
	if res.InternalLinks != 3 {
		t.Errorf("InternalLinks = %d, want 3", res.InternalLinks)
	}
	if res.ExternalLinks != 1 {
		t.Errorf("ExternalLinks = %d, want 1", res.ExternalLinks)
	}
}

func TestAnalyzeChecksLinks(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	var pageHits, externalHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		pageHits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<a href="#main">Skip</a>
<a href="/page">Home</a>
<a href="/ok">OK</a>
<a href="/missing">Missing</a>
<a href="/missing#again">Missing again</a>
<a href="http://example.com/ok">External</a>
<a href="http://example.com/missing">External missing</a>
<a href="%s/">Closed</a>
<a href="mailto:info@example.com">Mail</a>`, closed.URL)
	})
	mux.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) {})
	// ts.Client() sends requests for example.com here, which the hit count proves.
	mux.HandleFunc("example.com/", func(w http.ResponseWriter, r *http.Request) {
		externalHits.Add(1)
		if r.URL.Path != "/ok" {
			http.NotFound(w, r)
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	res, err := New(ts.Client()).Analyze(t.Context(), ts.URL+"/page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.InternalLinks != 6 {
		t.Errorf("InternalLinks = %d, want 6", res.InternalLinks)
	}
	if res.ExternalLinks != 2 {
		t.Errorf("ExternalLinks = %d, want 2", res.ExternalLinks)
	}
	// Both /missing links, the external missing one and the closed server.
	if res.InaccessibleLinks != 4 {
		t.Errorf("InaccessibleLinks = %d, want 4", res.InaccessibleLinks)
	}
	if res.UncheckedLinks != 0 {
		t.Errorf("UncheckedLinks = %d, want 0", res.UncheckedLinks)
	}
	if n := pageHits.Load(); n != 1 {
		t.Errorf("page fetched %d times, want 1", n)
	}
	if n := externalHits.Load(); n != 2 {
		t.Errorf("external links requested %d times, want 2", n)
	}
}

func TestAnalyzeInvalidURL(t *testing.T) {
	_, err := New(&http.Client{}).Analyze(t.Context(), "example.com")
	var invalidErr *InvalidURLError
	if !errors.As(err, &invalidErr) {
		t.Errorf("err = %v, want *InvalidURLError", err)
	}
}

func TestAnalyzeStatusError(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	_, err := New(ts.Client()).Analyze(t.Context(), ts.URL)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *StatusError", err)
	}
	if se.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", se.StatusCode, http.StatusNotFound)
	}
}
