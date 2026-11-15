package analyzer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// ErrNotHTML means the page was fetched but is not an HTML document.
var ErrNotHTML = errors.New("not an HTML page")

// InvalidURLError means the URL entered by the user was rejected.
type InvalidURLError struct {
	Reason string // short description for the user, such as "the URL has no host name"
}

func (e *InvalidURLError) Error() string {
	return "invalid URL: " + e.Reason
}

// StatusError means the page answered with a non-2xx status code.
type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("page returned status %d", e.StatusCode)
}

// UnreachableError means the request failed before a usable response was received.
type UnreachableError struct {
	Reason  string // short description for the user, such as "connection refused"
	Timeout bool
	Err     error
}

func (e *UnreachableError) Error() string {
	return "page unreachable: " + e.Reason + ": " + e.Err.Error()
}

func (e *UnreachableError) Unwrap() error {
	return e.Err
}

// unreachable wraps a request error with a reason the user can understand.
func unreachable(err error) *UnreachableError {
	e := &UnreachableError{Reason: "request failed", Err: err}
	var (
		netErr  net.Error
		dnsErr  *net.DNSError
		certErr *tls.CertificateVerificationError
	)
	// Timeouts come first so a DNS lookup that timed out counts as a timeout.
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		e.Reason = "request timed out"
		e.Timeout = true
	case errors.As(err, &dnsErr):
		e.Reason = "host name could not be resolved"
	case errors.Is(err, syscall.ECONNREFUSED):
		e.Reason = "connection refused"
	case errors.As(err, &certErr):
		e.Reason = "TLS certificate could not be verified"
	}
	return e
}
