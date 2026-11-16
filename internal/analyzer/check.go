package analyzer

import (
	"context"
	"net/http"
	"net/url"
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
	url   string
	state linkState
}

// checkLinks requests every unique link once, up to the link limit, and
// returns how many links are inaccessible and how many were left unchecked,
// both counted per occurrence on the page.
func (a *Analyzer) checkLinks(ctx context.Context, page *url.URL, links []*url.URL) (inaccessible, unchecked int) {
	urls, counts := uniqueLinks(page, links)
	if len(urls) == 0 {
		return 0, 0
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
				results <- linkResult{url: u, state: a.checkLink(ctx, u)}
			}
		})
	}
	wg.Wait()
	// Only the workers send on results, and they have all returned.
	close(results)

	for r := range results {
		switch r.state {
		case linkInaccessible:
			inaccessible += counts[r.url]
		case linkUnchecked:
			unchecked += counts[r.url]
		}
	}
	return inaccessible, unchecked
}

// checkLink GETs rawURL, following redirects, and reports whether it is
// accessible. ctx limits the whole check; the link also gets its own deadline.
func (a *Analyzer) checkLink(ctx context.Context, rawURL string) linkState {
	// Links still queued when the check is over are not requested at all, so
	// the workers empty the queue at once.
	if ctx.Err() != nil {
		return linkUnchecked
	}
	linkCtx, cancel := context.WithTimeout(ctx, a.linkTimeout)
	defer cancel()

	// GET rather than HEAD, which many servers answer with 403, 404 or 405
	// for pages that work in a browser.
	req, err := http.NewRequestWithContext(linkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return linkInaccessible
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		// Only the link's own timeout counts against it. If the whole check
		// ran out of time or was cancelled, the link may well be fine.
		if ctx.Err() != nil {
			return linkUnchecked
		}
		return linkInaccessible
	}
	// Only the status matters, so the body is closed unread.
	resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return linkInaccessible
	}
	return linkAccessible
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
