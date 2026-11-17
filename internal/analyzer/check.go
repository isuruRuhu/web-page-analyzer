package analyzer

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	// defaultWorkers is how many links are checked at the same time.
	defaultWorkers = 20
	// defaultMaxLinks is how many different links are checked per page. The
	// rest are reported as unchecked.
	defaultMaxLinks = 500
	// defaultLinkTimeout limits how long checking one link may take.
	defaultLinkTimeout = 5 * time.Second
	// defaultCheckTimeout limits how long checking all the links on a page may
	// take, so that with the page fetch an analysis stays within the server's
	// write timeout.
	defaultCheckTimeout = 30 * time.Second
)

// linkState is the outcome of checking one link.
type linkState int

const (
	linkAccessible linkState = iota
	linkInaccessible
	linkUnchecked // not known, since the whole check ran out of time or was cancelled
)

// linkResult is what a worker reports for one link.
type linkResult struct {
	url    string
	state  linkState
	reason string // why the link is inaccessible
}

// FailedLink is a link that turned out to be inaccessible.
type FailedLink struct {
	URL    string
	Reason string // the status, such as "404 Not Found", or why the request failed
	Count  int    // how many times the link appears on the page
}

// checkLinks requests every unique link once, up to the link limit. It
// returns how many links are inaccessible and how many were left unchecked,
// both counted per occurrence on the page, and the inaccessible links in page
// order.
func (a *Analyzer) checkLinks(ctx context.Context, page *url.URL, links []*url.URL) (inaccessible, unchecked int, failed []FailedLink) {
	urls, counts := uniqueLinks(page, links)
	if len(urls) == 0 {
		return 0, 0, nil
	}
	if len(urls) > a.maxLinks {
		for _, u := range urls[a.maxLinks:] {
			unchecked += counts[u]
		}
		urls = urls[:a.maxLinks]
	}

	ctx, cancel := context.WithTimeout(ctx, a.checkTimeout)
	defer cancel()

	// Both channels have room for every link, so queueing the jobs here never
	// waits for a worker and a worker never waits to hand in a result.
	jobs := make(chan string, len(urls))
	for _, u := range urls {
		jobs <- u
	}
	close(jobs)
	results := make(chan linkResult, len(urls))

	var wg sync.WaitGroup
	for range min(a.workers, len(urls)) {
		wg.Go(func() {
			for u := range jobs {
				state, reason := a.checkLink(ctx, u)
				results <- linkResult{url: u, state: state, reason: reason}
			}
		})
	}
	wg.Wait()
	// Only the workers send on results, and they have all returned.
	close(results)

	// Results arrive in the order the workers finish, so the reasons are
	// looked up by URL to list the failed links in page order.
	reasons := make(map[string]string)
	for r := range results {
		switch r.state {
		case linkInaccessible:
			inaccessible += counts[r.url]
			reasons[r.url] = r.reason
		case linkUnchecked:
			unchecked += counts[r.url]
		}
	}
	for _, u := range urls {
		if reason, ok := reasons[u]; ok {
			failed = append(failed, FailedLink{URL: u, Reason: reason, Count: counts[u]})
		}
	}
	return inaccessible, unchecked, failed
}

// checkLink GETs rawURL, following redirects, and reports whether it is
// accessible and, if it is not, why. ctx limits the whole check; the link
// also gets its own deadline.
func (a *Analyzer) checkLink(ctx context.Context, rawURL string) (linkState, string) {
	// Links still queued when the check is over are not requested at all, so
	// the workers empty the queue at once.
	if ctx.Err() != nil {
		return linkUnchecked, ""
	}
	linkCtx, cancel := context.WithTimeout(ctx, a.linkTimeout)
	defer cancel()

	// GET rather than HEAD, which many servers answer with 403, 404 or 405
	// for pages that work in a browser.
	req, err := http.NewRequestWithContext(linkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return linkInaccessible, "invalid URL"
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		// Only the link's own timeout counts against it. If the whole check
		// ran out of time or was cancelled, the link may well be fine.
		if ctx.Err() != nil {
			return linkUnchecked, ""
		}
		if errors.Is(err, ErrPrivateAddress) {
			return linkInaccessible, "private or local address"
		}
		return linkInaccessible, unreachable(err).Reason
	}
	// Only the status matters, so the body is closed unread.
	resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return linkInaccessible, statusReason(resp.StatusCode)
	}
	return linkAccessible, ""
}

// statusReason returns a status code with its standard text, if it has one.
func statusReason(code int) string {
	if text := http.StatusText(code); text != "" {
		return strconv.Itoa(code) + " " + text
	}
	return strconv.Itoa(code)
}

// uniqueLinks returns the links to check, each once in the form given by
// linkKey, in page order, and how many times each appears. Links to the page itself are
// left out, since it was just fetched.
func uniqueLinks(page *url.URL, links []*url.URL) ([]string, map[string]int) {
	self := linkKey(page)
	var urls []string
	counts := make(map[string]int)
	for _, link := range links {
		u := linkKey(link)
		if u == self {
			continue
		}
		if counts[u] == 0 {
			urls = append(urls, u)
		}
		counts[u]++
	}
	return urls, counts
}

// linkKey returns u as a string without its fragment and with "/" for an
// empty path, so that https://example.com#top and https://example.com/ are
// the same link.
func linkKey(u *url.URL) string {
	v := *u
	v.Fragment = ""
	v.RawFragment = ""
	if v.Path == "" {
		v.Path = "/"
	}
	return v.String()
}
