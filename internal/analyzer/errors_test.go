package analyzer

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "invalid URL", err: &InvalidURLError{Reason: "the URL is empty"}, want: "invalid URL: the URL is empty"},
		{name: "status", err: &StatusError{StatusCode: http.StatusNotFound}, want: "page returned status 404"},
		{name: "unreachable", err: &UnreachableError{Reason: "connection refused", Err: io.EOF}, want: "page unreachable: connection refused: EOF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnreachableErrorUnwrap(t *testing.T) {
	var err error = &UnreachableError{Reason: "request failed", Err: io.EOF}
	if !errors.Is(err, io.EOF) {
		t.Error("errors.Is(err, io.EOF) = false, want true")
	}
}
