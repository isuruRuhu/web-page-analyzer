package analyzer

import (
	"net/url"
	"strings"
)

// baseURL returns the URL that relative links resolve against: baseHref
// resolved against the page URL, or the page URL if baseHref is empty or
// cannot be parsed.
func baseURL(pageURL *url.URL, baseHref string) *url.URL {
	if baseHref == "" {
		return pageURL
	}
	base, err := pageURL.Parse(baseHref)
	if err != nil {
		return pageURL
	}
	return base
}

// resolveLinks resolves hrefs against base and returns the http and https
// links in order, duplicates included. Hrefs that cannot be parsed or have no
// host are skipped.
func resolveLinks(base *url.URL, hrefs []string) []*url.URL {
	var links []*url.URL
	for _, href := range hrefs {
		u, err := base.Parse(href)
		if err != nil {
			continue
		}
		if (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" {
			links = append(links, u)
		}
	}
	return links
}

// sameHost reports whether a and b have the same host name, ignoring case,
// one leading "www." on either side, the port and the scheme.
func sameHost(a, b *url.URL) bool {
	hostA := strings.TrimPrefix(strings.ToLower(a.Hostname()), "www.")
	hostB := strings.TrimPrefix(strings.ToLower(b.Hostname()), "www.")
	return hostA == hostB
}
