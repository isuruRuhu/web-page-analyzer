package analyzer

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// parseURL checks the URL entered by the user and returns it parsed.
// Only absolute http and https URLs are accepted.
func parseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &InvalidURLError{Reason: "the URL is empty"}
	}
	// Checked before url.Parse, whose message for control characters names its package.
	if strings.ContainsFunc(raw, unicode.IsControl) {
		return nil, &InvalidURLError{Reason: "the URL contains a control character"}
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Error repeats the whole input; its inner error says what is wrong.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, &InvalidURLError{Reason: err.Error()}
	}
	switch {
	case u.Scheme == "":
		return nil, &InvalidURLError{Reason: "the URL has no http:// or https:// prefix"}
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, &InvalidURLError{Reason: fmt.Sprintf("the URL must use http or https, not %q", u.Scheme)}
	// Hostname rather than Host, so "http://:8080" is rejected too.
	case u.Hostname() == "":
		return nil, &InvalidURLError{Reason: "the URL has no host name"}
	}
	return u, nil
}
