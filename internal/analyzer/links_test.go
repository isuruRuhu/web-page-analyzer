package analyzer

import (
	"slices"
	"testing"
)

func TestParseLinks(t *testing.T) {
	tests := []struct {
		name         string
		page         string // URL the document was fetched from, https://example.com/docs/guide if empty
		doc          string
		wantInternal int
		wantExternal int
	}{
		{name: "no links", doc: "<p>No links here</p>"},
		{name: "absolute same host", doc: `<a href="https://example.com/about">a</a>`, wantInternal: 1},
		{name: "same host other scheme", doc: `<a href="http://example.com/about">a</a>`, wantInternal: 1},
		{name: "same host with port", doc: `<a href="https://example.com:8443/about">a</a>`, wantInternal: 1},
		{name: "www on link only", doc: `<a href="https://www.example.com/about">a</a>`, wantInternal: 1},
		{name: "www on page only", page: "https://www.example.com/", doc: `<a href="https://example.com/about">a</a>`, wantInternal: 1},
		{name: "www on both", page: "https://www.example.com/", doc: `<a href="https://www.example.com/about">a</a>`, wantInternal: 1},
		{name: "uppercase host", doc: `<a href="https://EXAMPLE.COM/about">a</a>`, wantInternal: 1},
		{name: "uppercase www", doc: `<a href="https://WWW.Example.com/about">a</a>`, wantInternal: 1},
		{name: "only one www is ignored", doc: `<a href="https://www.www.example.com/">a</a>`, wantExternal: 1},
		{name: "other subdomain", doc: `<a href="https://blog.example.com/">a</a>`, wantExternal: 1},
		{name: "other host", doc: `<a href="https://golang.org/">a</a>`, wantExternal: 1},
		{name: "host ending with page host", doc: `<a href="https://notexample.com/">a</a>`, wantExternal: 1},
		{name: "host containing page host", doc: `<a href="https://example.com.evil.org/">a</a>`, wantExternal: 1},
		{name: "page on www linking to bare domain", page: "https://www.example.com/docs/", doc: `<a href="http://example.com:8080/">a</a>`, wantInternal: 1},
		{name: "relative path", doc: `<a href="intro">a</a>`, wantInternal: 1},
		{name: "parent relative path", doc: `<a href="../x">a</a>`, wantInternal: 1},
		{name: "root relative path", doc: `<a href="/pricing">a</a>`, wantInternal: 1},
		{name: "scheme relative other host", doc: `<a href="//other.com/x">a</a>`, wantExternal: 1},
		{name: "scheme relative same host", doc: `<a href="//example.com/x">a</a>`, wantInternal: 1},
		{name: "query only", doc: `<a href="?page=2">a</a>`, wantInternal: 1},
		{name: "fragment only", doc: `<a href="#top">a</a>`, wantInternal: 1},
		{name: "link to the page itself", doc: `<a href="https://example.com/docs/guide">a</a>`, wantInternal: 1},
		{name: "surrounding whitespace", doc: "<a href=\" \n https://other.com/ \t\">a</a>", wantExternal: 1},
		{name: "uppercase tag and scheme", doc: `<A HREF="HTTPS://other.com/">a</A>`, wantExternal: 1},
		{name: "empty href", doc: `<a href="">a</a>`},
		{name: "whitespace only href", doc: "<a href=\" \n\t \">a</a>"},
		{name: "no href", doc: `<a name="top">a</a>`},
		{name: "mailto", doc: `<a href="mailto:info@example.com">a</a>`},
		{name: "tel", doc: `<a href="tel:+15551234567">a</a>`},
		{name: "javascript", doc: `<a href="javascript:void(0)">a</a>`},
		{name: "data", doc: `<a href="data:text/plain,hello">a</a>`},
		{name: "ftp on same host", doc: `<a href="ftp://example.com/file.txt">a</a>`},
		{name: "unparsable", doc: `<a href="http://exa mple.com/">a</a>`},
		{name: "duplicates are counted", doc: `<a href="/a">a</a><a href="/a">a</a><a href="https://other.com/">b</a>`, wantInternal: 2, wantExternal: 1},
		{name: "inside svg", doc: `<svg><a href="https://other.com/">a</a></svg>`},
		{name: "inside svg foreignObject", doc: `<svg><foreignObject><a href="/x">a</a></foreignObject></svg>`, wantInternal: 1},
		{name: "link and area elements", doc: `<link rel="stylesheet" href="/style.css"><map name="m"><area href="https://other.com/"></map>`},
		{
			name: "mixed page",
			doc: `<nav><a href="/">Home</a><a href="/blog/">Blog</a><a href="#main">Skip</a></nav>
<p><a href="https://github.com/example">GitHub</a> <a href="mailto:hi@example.com">Mail</a>
<a href="https://www.example.com/contact">Contact</a> <a href="https://twitter.com/example">Twitter</a></p>`,
			wantInternal: 4,
			wantExternal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := tt.page
			if page == "" {
				page = "https://example.com/docs/guide"
			}
			res, links, err := parse([]byte(tt.doc), mustParse(t, page))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if res.InternalLinks != tt.wantInternal {
				t.Errorf("InternalLinks = %d, want %d", res.InternalLinks, tt.wantInternal)
			}
			if res.ExternalLinks != tt.wantExternal {
				t.Errorf("ExternalLinks = %d, want %d", res.ExternalLinks, tt.wantExternal)
			}
			if want := tt.wantInternal + tt.wantExternal; len(links) != want {
				t.Errorf("parse returned %d links, want %d", len(links), want)
			}
		})
	}
}

func TestParseLinksWithBase(t *testing.T) {
	page := mustParse(t, "https://example.com/docs/guide")

	tests := []struct {
		name         string
		doc          string
		wantInternal int
		wantExternal int
	}{
		{
			// Links are still compared with the page's host, not the base's.
			name:         "base on another host",
			doc:          `<base href="https://cdn.other.com/"><a href="x">a</a><a href="https://cdn.other.com/y">b</a><a href="https://example.com/z">c</a>`,
			wantInternal: 1,
			wantExternal: 2,
		},
		{
			// These resolve against the base, so they point to its host.
			name:         "fragment and query only with base on another host",
			doc:          `<base href="https://other.com/"><a href="#top">a</a><a href="?page=2">b</a>`,
			wantExternal: 2,
		},
		{name: "base href with surrounding whitespace", doc: "<base href=\" \n https://other.com/ \"><a href=\"x\">a</a>", wantExternal: 1},
		{name: "whitespace only base href then valid one", doc: `<base href="  "><base href="https://other.com/"><a href="x">a</a>`, wantExternal: 1},
		{name: "relative base", doc: `<base href="/assets/"><a href="x">a</a>`, wantInternal: 1},
		{name: "scheme relative base", doc: `<base href="//other.com/"><a href="x">a</a>`, wantExternal: 1},
		{name: "empty base href then valid one", doc: `<base href=""><base href="https://other.com/"><a href="x">a</a>`, wantExternal: 1},
		{name: "base without href then valid one", doc: `<base target="_blank"><base href="https://other.com/"><a href="x">a</a>`, wantExternal: 1},
		{name: "only the first base is used", doc: `<base href="https://other.com/"><base href="https://example.com/"><a href="x">a</a>`, wantExternal: 1},
		{name: "unparsable base falls back to page", doc: `<base href="http://exa mple.com/"><base href="https://other.com/"><a href="x">a</a>`, wantInternal: 1},
		{
			// The base applies to the whole document, as in browsers.
			name:         "base after links",
			doc:          `<a href="/x">a</a><base href="https://other.com/"><a href="/y">b</a>`,
			wantExternal: 2,
		},
		{
			// Relative links resolve to mailto URLs and are dropped.
			name:         "base with another scheme",
			doc:          `<base href="mailto:info@example.com"><a href="x">a</a><a href="https://example.com/">b</a>`,
			wantInternal: 1,
		},
		{name: "base inside svg", doc: `<svg><base href="https://other.com/"></base></svg><a href="x">a</a>`, wantInternal: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, _, err := parse([]byte(tt.doc), page)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if res.InternalLinks != tt.wantInternal {
				t.Errorf("InternalLinks = %d, want %d", res.InternalLinks, tt.wantInternal)
			}
			if res.ExternalLinks != tt.wantExternal {
				t.Errorf("ExternalLinks = %d, want %d", res.ExternalLinks, tt.wantExternal)
			}
		})
	}
}

func TestBaseURL(t *testing.T) {
	page := mustParse(t, "https://example.com/docs/guide")

	tests := []struct {
		name string
		href string
		want string
	}{
		{name: "no base", href: "", want: "https://example.com/docs/guide"},
		{name: "absolute", href: "https://cdn.other.com/assets/", want: "https://cdn.other.com/assets/"},
		{name: "root relative", href: "/assets/", want: "https://example.com/assets/"},
		{name: "relative", href: "v2/", want: "https://example.com/docs/v2/"},
		{name: "scheme relative", href: "//other.com/", want: "https://other.com/"},
		{name: "unparsable", href: "http://exa mple.com/", want: "https://example.com/docs/guide"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := baseURL(page, tt.href).String(); got != tt.want {
				t.Errorf("baseURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveLinks(t *testing.T) {
	tests := []struct {
		name  string
		base  string
		hrefs []string
		want  []string
	}{
		{
			name:  "relative forms",
			base:  "https://example.com/docs/guide",
			hrefs: []string{"intro", "../x", "/pricing", "//other.com/x", "?page=2", "#top"},
			want: []string{
				"https://example.com/docs/intro",
				"https://example.com/x",
				"https://example.com/pricing",
				"https://other.com/x",
				"https://example.com/docs/guide?page=2",
				"https://example.com/docs/guide#top",
			},
		},
		{
			name:  "against a base on another host",
			base:  "https://cdn.other.com/assets/",
			hrefs: []string{"app.js", "/", "#top"},
			want:  []string{"https://cdn.other.com/assets/app.js", "https://cdn.other.com/", "https://cdn.other.com/assets/#top"},
		},
		{
			name:  "absolute",
			base:  "https://example.com/",
			hrefs: []string{"http://example.com:8080/a", "HTTPS://Other.com/b"},
			want:  []string{"http://example.com:8080/a", "https://Other.com/b"},
		},
		{
			name:  "no host",
			base:  "https://example.com/",
			hrefs: []string{"http:foo", "https://", "http:///x"},
			want:  nil,
		},
		{
			name:  "other schemes are dropped",
			base:  "https://example.com/",
			hrefs: []string{"mailto:info@example.com", "tel:+15551234567", "javascript:void(0)", "data:text/plain,hi", "ftp://example.com/f"},
		},
		{
			name:  "unparsable is dropped",
			base:  "https://example.com/",
			hrefs: []string{"/a", "http://exa mple.com/", "/b"},
			want:  []string{"https://example.com/a", "https://example.com/b"},
		},
		{
			name:  "order and duplicates kept",
			base:  "https://example.com/",
			hrefs: []string{"/b", "/a", "/b"},
			want:  []string{"https://example.com/b", "https://example.com/a", "https://example.com/b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, u := range resolveLinks(mustParse(t, tt.base), tt.hrefs) {
				got = append(got, u.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("resolveLinks = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSameHost(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "same host", a: "https://example.com/a", b: "https://example.com/b", want: true},
		{name: "other scheme", a: "http://example.com/", b: "https://example.com/", want: true},
		{name: "other port", a: "https://example.com:8443/", b: "https://example.com/", want: true},
		{name: "case", a: "https://EXAMPLE.com/", b: "https://example.COM/", want: true},
		{name: "www on first", a: "https://www.example.com/", b: "https://example.com/", want: true},
		{name: "www on second", a: "https://example.com/", b: "https://www.example.com/", want: true},
		{name: "www on both", a: "https://www.example.com/", b: "https://www.example.com/", want: true},
		{name: "uppercase www", a: "https://WWW.Example.COM/", b: "https://example.com/", want: true},
		{name: "only one www is ignored", a: "https://www.www.example.com/", b: "https://example.com/", want: false},
		{name: "www without dot", a: "https://wwwexample.com/", b: "https://example.com/", want: false},
		{name: "subdomain", a: "https://blog.example.com/", b: "https://example.com/", want: false},
		{name: "ends with host", a: "https://notexample.com/", b: "https://example.com/", want: false},
		{name: "contains host", a: "https://example.com.evil.org/", b: "https://example.com/", want: false},
		{name: "other host", a: "https://golang.org/", b: "https://example.com/", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameHost(mustParse(t, tt.a), mustParse(t, tt.b)); got != tt.want {
				t.Errorf("sameHost(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
