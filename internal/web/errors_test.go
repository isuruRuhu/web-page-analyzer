package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"testing"

	"github.com/isuruRuhu/web-page-analyzer/internal/analyzer"
)

func TestDescribeError(t *testing.T) {
	const statusMsg = "The page responded with this error status, so it could not be analyzed."

	tests := []struct {
		name       string
		err        error
		wantStatus int
		want       problem
	}{
		{
			name:       "invalid URL",
			err:        &analyzer.InvalidURLError{Reason: "the URL has no host name"},
			wantStatus: http.StatusBadRequest,
			want:       problem{Status: "400 Bad Request", Message: "The URL has no host name. Enter a full URL such as https://example.com."},
		},
		{
			name:       "wrapped invalid URL",
			err:        fmt.Errorf("analyze: %w", &analyzer.InvalidURLError{Reason: "the URL is empty"}),
			wantStatus: http.StatusBadRequest,
			want:       problem{Status: "400 Bad Request", Message: "The URL is empty. Enter a full URL such as https://example.com."},
		},
		{
			name:       "status error",
			err:        &analyzer.StatusError{StatusCode: http.StatusNotFound},
			wantStatus: http.StatusBadGateway,
			want:       problem{Status: "404 Not Found", Message: statusMsg},
		},
		{
			name:       "status without standard text",
			err:        &analyzer.StatusError{StatusCode: 599},
			wantStatus: http.StatusBadGateway,
			want:       problem{Status: "599", Message: statusMsg},
		},
		{
			name:       "timeout",
			err:        &analyzer.UnreachableError{Reason: "request timed out", Timeout: true, Err: os.ErrDeadlineExceeded},
			wantStatus: http.StatusGatewayTimeout,
			want:       problem{Status: "504 Gateway Timeout", Message: "The page could not be reached: request timed out."},
		},
		{
			name:       "unreachable",
			err:        &analyzer.UnreachableError{Reason: "connection refused", Err: syscall.ECONNREFUSED},
			wantStatus: http.StatusBadGateway,
			want:       problem{Status: "502 Bad Gateway", Message: "The page could not be reached: connection refused."},
		},
		{
			name:       "not HTML",
			err:        fmt.Errorf("%w: content type is %q", analyzer.ErrNotHTML, "image/png"),
			wantStatus: http.StatusUnprocessableEntity,
			want:       problem{Status: "422 Unprocessable Entity", Message: "The URL does not point to an HTML page, so it could not be analyzed."},
		},
		{
			name:       "unexpected",
			err:        errors.New("parse HTML: something odd"),
			wantStatus: http.StatusInternalServerError,
			want:       problem{Status: "500 Internal Server Error", Message: "Something went wrong while analyzing the page. Please try again later."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, p := describeError(tt.err)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if *p != tt.want {
				t.Errorf("problem = %+v, want %+v", *p, tt.want)
			}
		})
	}
}

func TestSentence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "ascii", in: "the URL is empty", want: "The URL is empty."},
		{name: "multi-byte first letter", in: "élan", want: "Élan."},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sentence(tt.in); got != tt.want {
				t.Errorf("sentence(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
