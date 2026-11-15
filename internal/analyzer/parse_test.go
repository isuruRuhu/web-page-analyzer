package analyzer

import (
	"maps"
	"reflect"
	"testing"
)

func mustParseHTML(t *testing.T, doc string) *Result {
	t.Helper()
	res, err := parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse %q: %v", doc, err)
	}
	return res
}

func TestParseEmptyDocument(t *testing.T) {
	want := &Result{
		HTMLVersion: "Unknown (no doctype)",
		Headings:    map[string]int{"h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
	}
	if got := mustParseHTML(t, ""); !reflect.DeepEqual(got, want) {
		t.Errorf("parse = %+v, want %+v", got, want)
	}
}

func TestParseHTMLVersion(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "html5", doc: "<!DOCTYPE html>", want: "HTML5"},
		{name: "html5 lowercase", doc: "<!doctype html>", want: "HTML5"},
		{name: "html5 legacy compat", doc: `<!DOCTYPE html SYSTEM "about:legacy-compat">`, want: "HTML5"},
		{name: "html 4.01 strict", doc: `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">`, want: "HTML 4.01 Strict"},
		{name: "html 4.01 transitional", doc: `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Transitional//EN" "http://www.w3.org/TR/html4/loose.dtd">`, want: "HTML 4.01 Transitional"},
		{name: "html 4.01 frameset", doc: `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01 Frameset//EN" "http://www.w3.org/TR/html4/frameset.dtd">`, want: "HTML 4.01 Frameset"},
		{name: "xhtml 1.0 strict", doc: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`, want: "XHTML 1.0 Strict"},
		{name: "xhtml 1.0 transitional", doc: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`, want: "XHTML 1.0 Transitional"},
		{name: "xhtml 1.0 frameset", doc: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Frameset//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd">`, want: "XHTML 1.0 Frameset"},
		{name: "xhtml 1.1", doc: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`, want: "XHTML 1.1"},
		{name: "html 3.2", doc: `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 3.2 Final//EN">`, want: "HTML 3.2"},
		{name: "html 2.0", doc: `<!DOCTYPE HTML PUBLIC "-//IETF//DTD HTML 2.0//EN">`, want: "HTML 2.0"},
		{name: "public identifier in lowercase", doc: `<!doctype html public "-//w3c//dtd html 4.01//en">`, want: "HTML 4.01 Strict"},
		{name: "xml declaration before doctype", doc: `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`, want: "XHTML 1.1"},
		{name: "comment before doctype", doc: "<!-- header --><!DOCTYPE html>", want: "HTML5"},
		{name: "byte order mark before doctype", doc: "\ufeff<!DOCTYPE html>", want: "HTML5"},
		{name: "no doctype", doc: "<html><head><title>t</title></head></html>", want: "Unknown (no doctype)"},
		{name: "doctype after text is dropped", doc: "hello<!DOCTYPE html>", want: "Unknown (no doctype)"},
		{name: "unknown public identifier", doc: `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.0//EN">`, want: "Unknown"},
		{name: "unknown name", doc: "<!DOCTYPE svg>", want: "Unknown"},
		{name: "empty doctype", doc: "<!DOCTYPE>", want: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustParseHTML(t, tt.doc).HTMLVersion; got != tt.want {
				t.Errorf("HTMLVersion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTitle(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{name: "normal", doc: "<html><head><title>Example Domain</title></head></html>", want: "Example Domain"},
		{name: "whitespace and newlines", doc: "<title>\n\t Example \n\n  Domain \t</title>", want: "Example Domain"},
		{name: "entities", doc: "<title>Tom &amp; Jerry &lt;3</title>", want: "Tom & Jerry <3"},
		{name: "tags inside are text", doc: "<title>a <b>bold</b> move</title>", want: "a <b>bold</b> move"},
		{name: "missing", doc: "<html><head></head><body><h1>Hi</h1></body></html>", want: ""},
		{name: "empty", doc: "<title></title>", want: ""},
		{name: "only whitespace", doc: "<title> \n </title>", want: ""},
		{name: "in body", doc: "<body><p>text</p><title>Late</title></body>", want: "Late"},
		{name: "first of two", doc: "<title>First</title><title>Second</title>", want: "First"},
		{name: "empty first of two", doc: "<title></title><title>Second</title>", want: ""},
		{name: "svg title before real one", doc: "<body><svg><title>Icon</title></svg><title>Real</title></body>", want: "Real"},
		{name: "svg title instead of real one", doc: "<body><svg><title>Icon</title></svg></body>", want: ""},
		{name: "svg title after real one", doc: "<head><title>Real</title></head><body><svg><title>Icon</title></svg></body>", want: "Real"},
		{name: "math title", doc: "<body><math><title>Formula</title></math></body>", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustParseHTML(t, tt.doc).Title; got != tt.want {
				t.Errorf("Title = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseHeadings(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want map[string]int
	}{
		{
			name: "none",
			doc:  "<p>No headings here</p>",
			want: map[string]int{"h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
		},
		{
			name: "mixed levels",
			doc:  "<h1>a</h1><h2>b</h2><h2>c</h2><h3>d</h3><h6>e</h6>",
			want: map[string]int{"h1": 1, "h2": 2, "h3": 1, "h4": 0, "h5": 0, "h6": 1},
		},
		{
			name: "nested",
			doc:  "<header><h1>a</h1></header><main><section><article><h2>b</h2><div><h4>c</h4></div></article></section></main>",
			want: map[string]int{"h1": 1, "h2": 1, "h3": 0, "h4": 1, "h5": 0, "h6": 0},
		},
		{
			name: "uppercase tags",
			doc:  "<H1>a</H1><H5>b</H5>",
			want: map[string]int{"h1": 1, "h2": 0, "h3": 0, "h4": 0, "h5": 1, "h6": 0},
		},
		{
			name: "similar names are not headings",
			doc:  "<header>a</header><hgroup>b</hgroup><h7>c</h7>",
			want: map[string]int{"h1": 0, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
		},
		{
			// The parser closes the svg when it sees the heading.
			name: "inside svg",
			doc:  "<svg><h1>a</h1></svg>",
			want: map[string]int{"h1": 1, "h2": 0, "h3": 0, "h4": 0, "h5": 0, "h6": 0},
		},
		{
			name: "inside svg foreignObject",
			doc:  "<svg><foreignObject><h3>a</h3></foreignObject></svg>",
			want: map[string]int{"h1": 0, "h2": 0, "h3": 1, "h4": 0, "h5": 0, "h6": 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustParseHTML(t, tt.doc).Headings; !maps.Equal(got, tt.want) {
				t.Errorf("Headings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLoginForm(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want bool
	}{
		{name: "one password field", doc: `<form action="/login"><input name="user"><input type="password" name="pass"><button>Sign in</button></form>`, want: true},
		{name: "two password fields", doc: `<form><input name="user"><input type="password" name="pass"><input type="password" name="confirm"></form>`, want: false},
		{name: "password outside form", doc: `<input name="user"><input type="password" name="pass">`, want: false},
		{name: "no form", doc: "<p>Hello</p>", want: false},
		{name: "form without password", doc: `<form><input type="search" name="q"></form>`, want: false},
		{name: "uppercase type", doc: `<FORM><INPUT TYPE="PASSWORD"></FORM>`, want: true},
		{name: "password deep inside form", doc: `<form><div><label>Password <input type="password"></label></div></form>`, want: true},
		{name: "only second form is login", doc: `<form><input type="search"></form><form><input type="password"></form>`, want: true},
		{name: "login and sign-up forms", doc: `<form><input type="password"></form><form><input type="password"><input type="password"></form>`, want: true},
		// The inner form tag is dropped, so both fields belong to the outer form.
		{name: "nested form", doc: `<form><input type="password"><form><input type="password"></form></form>`, want: false},
		// A misplaced </form> leaves the second form nested inside the first.
		{name: "login form around sign-up form", doc: `<form><input type="password"><div></form><form><input type="password"><input type="password"></form>`, want: true},
		{name: "form inside svg", doc: `<svg><form><input type="password"></form></svg>`, want: false},
		// The parser leaves the form empty and puts the input in the cell.
		{name: "form wrapping table rows", doc: `<table><form><tr><td><input type="password"></td></tr></form></table>`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustParseHTML(t, tt.doc).HasLoginForm; got != tt.want {
				t.Errorf("HasLoginForm = %v, want %v", got, tt.want)
			}
		})
	}
}
