package analyzer

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// htmlVersions maps lowercased doctype public identifiers to HTML versions.
var htmlVersions = map[string]string{
	"-//w3c//dtd html 4.01//en":              "HTML 4.01 Strict",
	"-//w3c//dtd html 4.01 transitional//en": "HTML 4.01 Transitional",
	"-//w3c//dtd html 4.01 frameset//en":     "HTML 4.01 Frameset",
	"-//w3c//dtd xhtml 1.0 strict//en":       "XHTML 1.0 Strict",
	"-//w3c//dtd xhtml 1.0 transitional//en": "XHTML 1.0 Transitional",
	"-//w3c//dtd xhtml 1.0 frameset//en":     "XHTML 1.0 Frameset",
	"-//w3c//dtd xhtml 1.1//en":              "XHTML 1.1",
	"-//w3c//dtd html 3.2 final//en":         "HTML 3.2",
	"-//ietf//dtd html 2.0//en":              "HTML 2.0",
}

// parse reads an HTML document and fills in every Result field except URL.
func parse(body []byte) (*Result, error) {
	// html.Parse treats a byte order mark as text, which hides the doctype.
	body = bytes.TrimPrefix(body, []byte("\ufeff"))
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	res := &Result{
		HTMLVersion: "Unknown (no doctype)",
		Headings:    map[string]int{"h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
	}
	var titleFound bool
	passwords := make(map[*html.Node]int) // password inputs per form
	for n := range doc.Descendants() {
		if n.Type == html.DoctypeNode {
			res.HTMLVersion = htmlVersion(n)
			continue
		}
		// Skip foreign elements, such as an <svg> and its <title>.
		if n.Type != html.ElementNode || n.Namespace != "" {
			continue
		}
		switch n.Data {
		case "title":
			if !titleFound {
				res.Title = textContent(n)
				titleFound = true
			}
		case "h1", "h2", "h3", "h4", "h5", "h6":
			res.Headings[n.Data]++
		case "input":
			if strings.EqualFold(attr(n, "type"), "password") {
				if form := enclosingForm(n); form != nil {
					passwords[form]++
				}
			}
		}
	}
	// Sign-up and password change forms have two or more password fields.
	for _, count := range passwords {
		if count == 1 {
			res.HasLoginForm = true
		}
	}
	return res, nil
}

// htmlVersion names the HTML version declared by a doctype node.
func htmlVersion(doctype *html.Node) string {
	public := attr(doctype, "public")
	// HTML5 has no public identifier, only an optional about:legacy-compat system one.
	if doctype.Data == "html" && public == "" {
		return "HTML5"
	}
	if v, ok := htmlVersions[strings.ToLower(public)]; ok {
		return v
	}
	return "Unknown"
}

// textContent returns the text inside n with runs of whitespace collapsed.
func textContent(n *html.Node) string {
	var b strings.Builder
	for c := range n.Descendants() {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// attr returns the value of n's attribute key, or "" if it has none.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// enclosingForm returns the nearest <form> element that contains n, or nil.
func enclosingForm(n *html.Node) *html.Node {
	for p := range n.Ancestors() {
		if p.Type == html.ElementNode && p.Data == "form" {
			return p
		}
	}
	return nil
}
