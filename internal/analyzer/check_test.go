package analyzer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// linksOn resolves hrefs as links found on the page at pageURL.
func linksOn(t *testing.T, pageURL string, hrefs ...string) []*url.URL {
	t.Helper()
	return resolveLinks(mustParse(t, pageURL), hrefs)
}

// waitForGoroutines fails the test if the number of goroutines does not drop
// to at most want within a few seconds.
func waitForGoroutines(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > want {
		t.Errorf("%d goroutines running, want at most %d", n, want)
	}
}

func TestUniqueLinks(t *testing.T) {
	tests := []struct {
		name       string
		page       string // https://example.com/docs/guide if empty
		hrefs      []string
		wantURLs   []string
		wantCounts map[string]int
	}{
		{name: "no links"},
		{
			name:       "duplicates in page order",
			hrefs:      []string{"/b", "/a", "/b", "/b"},
			wantURLs:   []string{"https://example.com/b", "https://example.com/a"},
			wantCounts: map[string]int{"https://example.com/b": 3, "https://example.com/a": 1},
		},
		{
			name:       "fragments are ignored",
			hrefs:      []string{"/a#one", "/a#two", "/a"},
			wantURLs:   []string{"https://example.com/a"},
			wantCounts: map[string]int{"https://example.com/a": 3},
		},
		{
			name:       "queries are kept",
			hrefs:      []string{"/a?page=1", "/a?page=2"},
			wantURLs:   []string{"https://example.com/a?page=1", "https://example.com/a?page=2"},
			wantCounts: map[string]int{"https://example.com/a?page=1": 1, "https://example.com/a?page=2": 1},
		},
		{
			name:       "links to the page itself are left out",
			hrefs:      []string{"#top", "guide", "https://example.com/docs/guide#intro", "?q=1"},
			wantURLs:   []string{"https://example.com/docs/guide?q=1"},
			wantCounts: map[string]int{"https://example.com/docs/guide?q=1": 1},
		},
		{
			name:       "empty path is the same as /",
			page:       "https://example.com",
			hrefs:      []string{"/", "https://example.com", "https://other.example", "https://other.example/"},
			wantURLs:   []string{"https://other.example/"},
			wantCounts: map[string]int{"https://other.example/": 2},
		},
		{
			name:       "page URL with a fragment",
			page:       "https://example.com/docs/guide#intro",
			hrefs:      []string{"https://example.com/docs/guide", "/other"},
			wantURLs:   []string{"https://example.com/other"},
			wantCounts: map[string]int{"https://example.com/other": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := tt.page
			if page == "" {
				page = "https://example.com/docs/guide"
			}
			urls, counts := uniqueLinks(mustParse(t, page), linksOn(t, page, tt.hrefs...))
			if !slices.Equal(urls, tt.wantURLs) {
				t.Errorf("urls = %q, want %q", urls, tt.wantURLs)
			}
			if !maps.Equal(counts, tt.wantCounts) {
				t.Errorf("counts = %v, want %v", counts, tt.wantCounts)
			}
		})
	}
}

func TestCheckLinks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/created", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/not-modified", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	mux.Handle("/missing", http.NotFoundHandler())
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.Handle("/moved", http.RedirectHandler("/ok", http.StatusMovedPermanently))
	mux.Handle("/moved-to-missing", http.RedirectHandler("/missing", http.StatusFound))
	mux.Handle("/loop", http.RedirectHandler("/loop", http.StatusFound))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	// Not served, so requesting it would count as inaccessible.
	page := ts.URL + "/page"

	tests := []struct {
		name             string
		hrefs            []string
		wantInaccessible int
	}{
		{name: "no links"},
		{name: "only links to the page itself", hrefs: []string{"/page", "#top"}},
		{name: "all accessible", hrefs: []string{"/ok", "/created", "/not-modified"}},
		{name: "redirect to a working page", hrefs: []string{"/moved"}},
		{name: "not found", hrefs: []string{"/missing"}, wantInaccessible: 1},
		{name: "server error", hrefs: []string{"/broken"}, wantInaccessible: 1},
		{name: "redirect to a missing page", hrefs: []string{"/moved-to-missing"}, wantInaccessible: 1},
		{name: "too many redirects", hrefs: []string{"/loop"}, wantInaccessible: 1},
		{name: "connection refused", hrefs: []string{closed.URL + "/"}, wantInaccessible: 1},
		{
			name:             "counted per occurrence",
			hrefs:            []string{"/ok", "/missing", "/broken", "/missing#again", "/ok", "/missing"},
			wantInaccessible: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			links := linksOn(t, page, tt.hrefs...)
			inaccessible, unchecked := New(ts.Client()).checkLinks(t.Context(), mustParse(t, page), links)
			if inaccessible != tt.wantInaccessible {
				t.Errorf("inaccessible = %d, want %d", inaccessible, tt.wantInaccessible)
			}
			if unchecked != 0 {
				t.Errorf("unchecked = %d, want 0", unchecked)
			}
		})
	}
}

func TestCheckLinkRequest(t *testing.T) {
	got := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Method + " " + r.UserAgent()
		// Send the status, then hold the body until the client hangs up.
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	a := New(ts.Client())
	a.linkTimeout = time.Minute

	start := time.Now()
	if state := a.checkLink(t.Context(), ts.URL); state != linkAccessible {
		t.Errorf("state = %d, want linkAccessible", state)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("checkLink took %v, want it not to wait for the body", elapsed)
	}
	if want := "GET " + userAgent; <-got != want {
		t.Errorf("request does not have method and user agent %q", want)
	}
}

func TestCheckLinkUnparsableURL(t *testing.T) {
	a := New(&http.Client{Transport: errTransport{err: errors.New("no request expected")}})
	if state := a.checkLink(t.Context(), "http://exa mple.com/"); state != linkInaccessible {
		t.Errorf("state = %d, want linkInaccessible", state)
	}
}

func TestCheckLinksRequestsEachURLOnce(t *testing.T) {
	var pageHits, okHits, missingHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(http.ResponseWriter, *http.Request) { pageHits.Add(1) })
	mux.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) { okHits.Add(1) })
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		missingHits.Add(1)
		http.NotFound(w, r)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	page := ts.URL + "/page"
	links := linksOn(t, page, "/missing", "/ok#a", "/missing", "/ok#b", "/ok", "/missing#c", "#top", "/page")
	inaccessible, unchecked := New(ts.Client()).checkLinks(t.Context(), mustParse(t, page), links)

	if inaccessible != 3 {
		t.Errorf("inaccessible = %d, want 3", inaccessible)
	}
	if unchecked != 0 {
		t.Errorf("unchecked = %d, want 0", unchecked)
	}
	if n := missingHits.Load(); n != 1 {
		t.Errorf("/missing requested %d times, want 1", n)
	}
	if n := okHits.Load(); n != 1 {
		t.Errorf("/ok requested %d times, want 1", n)
	}
	if n := pageHits.Load(); n != 0 {
		t.Errorf("/page requested %d times, want 0", n)
	}
}

func TestCheckLinksWorkers(t *testing.T) {
	tests := []struct {
		name    string
		workers int
		links   int
	}{
		{name: "fewer links than workers", workers: 20, links: 2},
		{name: "many more links than workers", workers: 3, links: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits, inFlight, maxInFlight atomic.Int32
			started := make(chan struct{}, tt.links)
			release := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				n := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					m := maxInFlight.Load()
					if n <= m || maxInFlight.CompareAndSwap(m, n) {
						break
					}
				}
				started <- struct{}{}
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}))
			defer ts.Close()

			a := New(ts.Client())
			a.workers = tt.workers
			// Long enough that a held request never times out.
			a.linkTimeout = time.Minute
			page := ts.URL + "/page"
			var hrefs []string
			for i := range tt.links {
				hrefs = append(hrefs, fmt.Sprintf("/%d", i))
			}
			links := linksOn(t, page, hrefs...)

			var inaccessible, unchecked int
			done := make(chan struct{})
			go func() {
				defer close(done)
				inaccessible, unchecked = a.checkLinks(t.Context(), mustParse(t, page), links)
			}()
			// Wait until as many requests are in flight as the workers allow,
			// then let them finish one at a time to stay at that limit.
			limit := min(tt.workers, tt.links)
			for range limit {
				<-started
			}
			for range tt.links - limit {
				release <- struct{}{}
				<-started
			}
			close(release)
			<-done

			if n := maxInFlight.Load(); n > int32(tt.workers) {
				t.Errorf("%d requests ran at once, want at most %d", n, tt.workers)
			} else if n < 2 {
				t.Errorf("%d requests ran at once, want more than 1", n)
			}
			if n := hits.Load(); n != int32(tt.links) {
				t.Errorf("%d requests, want %d", n, tt.links)
			}
			if inaccessible != 0 || unchecked != 0 {
				t.Errorf("inaccessible, unchecked = %d, %d, want 0, 0", inaccessible, unchecked)
			}
		})
	}
}

func TestCheckLinksTimeouts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) {})
	mux.Handle("/missing", http.NotFoundHandler())
	// Never answers and returns once the client gives up.
	mux.HandleFunc("/slow/", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	tests := []struct {
		name             string
		workers          int
		linkTimeout      time.Duration
		checkTimeout     time.Duration
		hrefs            []string
		wantInaccessible int
		wantUnchecked    int
	}{
		{
			// The link had all of its own time and did not answer.
			name:             "link timeout",
			workers:          defaultWorkers,
			linkTimeout:      50 * time.Millisecond,
			checkTimeout:     time.Minute,
			hrefs:            []string{"/slow/1", "/slow/1"},
			wantInaccessible: 2,
		},
		{
			// Both workers wait on slow links until the deadline, so /ok is
			// never requested. Only /missing gets an answer in time.
			name:             "overall timeout",
			workers:          2,
			linkTimeout:      time.Minute,
			checkTimeout:     500 * time.Millisecond,
			hrefs:            []string{"/missing", "/slow/1", "/slow/2", "/ok", "/slow/1"},
			wantInaccessible: 1,
			wantUnchecked:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := New(ts.Client())
			a.workers = tt.workers
			a.linkTimeout = tt.linkTimeout
			a.checkTimeout = tt.checkTimeout
			page := ts.URL + "/page"

			inaccessible, unchecked := a.checkLinks(t.Context(), mustParse(t, page), linksOn(t, page, tt.hrefs...))
			if inaccessible != tt.wantInaccessible {
				t.Errorf("inaccessible = %d, want %d", inaccessible, tt.wantInaccessible)
			}
			if unchecked != tt.wantUnchecked {
				t.Errorf("unchecked = %d, want %d", unchecked, tt.wantUnchecked)
			}
		})
	}
}

func TestCheckLinksCancel(t *testing.T) {
	before := runtime.NumGoroutine()

	var okHits atomic.Int32
	started := make(chan struct{}, 2)
	mux := http.NewServeMux()
	mux.Handle("/missing", http.NotFoundHandler())
	mux.HandleFunc("/ok", func(http.ResponseWriter, *http.Request) { okHits.Add(1) })
	mux.HandleFunc("/slow/", func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	a := New(ts.Client())
	a.workers = 2
	// Long enough that only the cancel can stop the slow links.
	a.linkTimeout = time.Minute
	a.checkTimeout = time.Minute

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		// With two workers, both slow links are in flight only after /missing is done.
		<-started
		<-started
		cancel()
	}()

	page := ts.URL + "/page"
	links := linksOn(t, page, "/missing", "/slow/1", "/slow/2", "/ok", "/slow/1")
	start := time.Now()
	inaccessible, unchecked := a.checkLinks(ctx, mustParse(t, page), links)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("checkLinks took %v, want it to return soon after cancel", elapsed)
	}
	if inaccessible != 1 {
		t.Errorf("inaccessible = %d, want 1", inaccessible)
	}
	if unchecked != 4 {
		t.Errorf("unchecked = %d, want 4", unchecked)
	}
	if n := okHits.Load(); n != 0 {
		t.Errorf("/ok requested %d times after cancel, want 0", n)
	}

	ts.Close()
	waitForGoroutines(t, before)
}

func TestCheckLinksLimit(t *testing.T) {
	var requests atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer ts.Close()

	// Five different links, the last of them twice, with room for three.
	page := ts.URL + "/page"
	links := linksOn(t, page, "/1", "/2", "/3", "/4", "/5", "/5")
	a := New(ts.Client())
	a.maxLinks = 3

	inaccessible, unchecked := a.checkLinks(t.Context(), mustParse(t, page), links)
	if inaccessible != 0 || unchecked != 3 {
		t.Errorf("inaccessible, unchecked = %d, %d, want 0, 3", inaccessible, unchecked)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("server got %d requests, want 3", got)
	}
}
