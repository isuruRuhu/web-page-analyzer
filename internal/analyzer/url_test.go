package analyzer

import (
	"errors"
	"testing"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		want       string
		wantReason string
	}{
		{name: "http", raw: "http://example.com", want: "http://example.com"},
		{name: "port path and query", raw: "https://example.com:8443/a/b?q=1&r=2", want: "https://example.com:8443/a/b?q=1&r=2"},
		{name: "surrounding whitespace", raw: " \t https://example.com/ \n", want: "https://example.com/"},
		{name: "uppercase scheme", raw: "HTTPS://example.com", want: "https://example.com"},
		{name: "empty", raw: "", wantReason: "the URL is empty"},
		{name: "only whitespace", raw: " \t\n", wantReason: "the URL is empty"},
		{name: "missing scheme", raw: "example.com/page", wantReason: "the URL has no http:// or https:// prefix"},
		{name: "scheme relative", raw: "//example.com", wantReason: "the URL has no http:// or https:// prefix"},
		{name: "unsupported scheme", raw: "ftp://example.com/file", wantReason: `the URL must use http or https, not "ftp"`},
		{name: "host and port without scheme", raw: "localhost:8080", wantReason: `the URL must use http or https, not "localhost"`},
		{name: "ip and port without scheme", raw: "127.0.0.1:8080", wantReason: "first path segment in URL cannot contain colon"},
		{name: "missing host", raw: "http://", wantReason: "the URL has no host name"},
		{name: "path without host", raw: "https:///page", wantReason: "the URL has no host name"},
		{name: "port without host", raw: "http://:8080", wantReason: "the URL has no host name"},
		{name: "no slashes after scheme", raw: "https:example.com", wantReason: "the URL has no host name"},
		{name: "unparsable", raw: "http://exa mple.com", wantReason: `invalid character " " in host name`},
		{name: "bad port", raw: "http://example.com:abc", wantReason: `invalid port ":abc" after host`},
		{name: "control character", raw: "http://exa\x7fmple.com", wantReason: "the URL contains a control character"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseURL(tt.raw)
			if tt.wantReason != "" {
				var ie *InvalidURLError
				if !errors.As(err, &ie) {
					t.Fatalf("err = %v, want *InvalidURLError", err)
				}
				if ie.Reason != tt.wantReason {
					t.Errorf("Reason = %q, want %q", ie.Reason, tt.wantReason)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if u.String() != tt.want {
				t.Errorf("url = %q, want %q", u.String(), tt.want)
			}
		})
	}
}
