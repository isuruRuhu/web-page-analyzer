// Package analyzer fetches a web page and reports its HTML version, title,
// heading counts, internal and external link counts, how many of those links
// are inaccessible and whether it has a login form.
package analyzer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/html/charset"
)

// maxBodySize caps how much of a page is read; anything beyond it is ignored.
const maxBodySize = 10 << 20 // 10 MB

// userAgent identifies the analyzer to the sites it fetches.
const userAgent = "web-page-analyzer/1.0 (+https://github.com/isuruRuhu/web-page-analyzer)"

// Analyzer fetches and analyzes web pages with its HTTP client.
type Analyzer struct {
	client *http.Client

	// Limits for checking links, set to the defaults by New.
	workers      int
	maxLinks     int
	linkTimeout  time.Duration
	checkTimeout time.Duration
}

// New returns an Analyzer that makes its requests with client.
func New(client *http.Client) *Analyzer {
	return &Analyzer{
		client:       client,
		workers:      defaultWorkers,
		maxLinks:     defaultMaxLinks,
		linkTimeout:  defaultLinkTimeout,
		checkTimeout: defaultCheckTimeout,
	}
}

// Result is what Analyze found on a page.
type Result struct {
	URL               string // final URL after redirects
	HTMLVersion       string
	Title             string
	Headings          map[string]int // count per level, keyed "h1" to "h6"
	InternalLinks     int            // http(s) links to the page's own host
	ExternalLinks     int            // http(s) links to any other host
	InaccessibleLinks int            // links that failed or answered with status 400 or above
	UncheckedLinks    int            // links left unchecked by the link limit, time limit or cancellation
	HasLoginForm      bool           // a form with exactly one password field
}

// Analyze fetches the page at rawURL and reports what it contains.
func (a *Analyzer) Analyze(ctx context.Context, rawURL string) (*Result, error) {
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	final, body, err := a.fetch(ctx, u)
	if err != nil {
		return nil, err
	}
	res, links, err := parse(body, final)
	if err != nil {
		return nil, err
	}
	res.URL = final.String()
	res.InaccessibleLinks, res.UncheckedLinks = a.checkLinks(ctx, final, links)
	return res, nil
}

// fetch GETs u and returns the URL it ended up at after redirects and the
// first maxBodySize bytes of the body, decoded to UTF-8.
func (a *Analyzer) fetch(ctx context.Context, u *url.URL) (*url.URL, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		// Do returns both a response and an error only when it gives up
		// following redirects, and the body is already closed.
		if resp != nil {
			return nil, nil, &UnreachableError{Reason: "too many redirects", Err: err}
		}
		if errors.Is(err, ErrPrivateAddress) {
			return nil, nil, fmt.Errorf("fetch %s: %w", u.Host, ErrPrivateAddress)
		}
		return nil, nil, unreachable(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil, &StatusError{StatusCode: resp.StatusCode}
	}
	ct := resp.Header.Get("Content-Type")
	if !isHTML(ct) {
		return nil, nil, fmt.Errorf("%w: content type is %q", ErrNotHTML, ct)
	}

	// NewReader reads the start of the body to detect the encoding, so read
	// errors can show up here. It returns io.EOF for an empty body.
	r, err := charset.NewReader(io.LimitReader(resp.Body, maxBodySize), ct)
	if errors.Is(err, io.EOF) {
		return resp.Request.URL, nil, nil
	}
	if err != nil {
		return nil, nil, unreachable(fmt.Errorf("read body: %w", err))
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, unreachable(fmt.Errorf("read body: %w", err))
	}
	return resp.Request.URL, body, nil
}

// isHTML reports whether a Content-Type header value describes an HTML page.
// An empty value is accepted since the body may still be HTML.
func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}
	// A broken parameter, such as "text/html; charset", still leaves a usable
	// media type.
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil && !errors.Is(err, mime.ErrInvalidMediaParameter) {
		return false
	}
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}
