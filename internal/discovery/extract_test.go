package discovery

import (
	"reflect"
	"strings"
	"testing"
)

func TestExtractURLs_PostFilterLimit(t *testing.T) {
	html := `
<html><body>
  <a href="/file1.zip">File 1</a>
  <a href="/file2.zip">Skip</a>
  <a href="/file3.zip">File 3</a>
</body></html>`

	got, err := ExtractURLs("https://example.com", strings.NewReader(html), ExtractOptions{
		Selector:             "a",
		DefaultAttrMappings:  map[string]string{"a": "href"},
		RegexFilter:          `File \d`,
		SelectorMatchesLimit: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/file1.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_CustomAttr(t *testing.T) {
	html := `<a class="download" data-download-url="https://example.com/a.zip">Download</a>`
	got, err := ExtractURLs("https://example.com", strings.NewReader(html), ExtractOptions{
		Selector: ".download",
		Attr:     "data-download-url",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/a.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_AttrRegex(t *testing.T) {
	html := `<a href="https://example.com/file1.zip">1</a><a href="https://example.com/random.zip">2</a>`
	got, err := ExtractURLs("https://example.com", strings.NewReader(html), ExtractOptions{
		Selector:            "a",
		DefaultAttrMappings: map[string]string{"a": "href"},
		AttrValRegexFilter:  `file\d\.zip`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/file1.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_DefaultAttrMapping_WithComplexSelector(t *testing.T) {
	html := `<div class="download-link"><a href="/doc.pdf">Download</a></div>`
	got, err := ExtractURLs("https://example.com", strings.NewReader(html), ExtractOptions{
		Selector:            ".download-link a",
		DefaultAttrMappings: map[string]string{"a": "href"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/doc.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_SkipsNonHTTPLinksAndStripsFragments(t *testing.T) {
	html := `
<a href="mailto:me@example.com">mail</a>
<a href="javascript:void(0)">js</a>
<a href="data:text/plain,hi">data</a>
<a href="#top">top</a>
<a href="/doc.pdf#page=2">doc</a>
<a href=" /spaced.pdf ">spaced</a>`
	got, err := ExtractURLs("https://example.com/list/", strings.NewReader(html), ExtractOptions{
		Selector:             "a",
		DefaultAttrMappings:  map[string]string{"a": "href"},
		SelectorMatchesLimit: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The limit counts only usable links.
	want := []string{"https://example.com/doc.pdf", "https://example.com/spaced.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_HonorsBaseHref(t *testing.T) {
	html := `<html><head><base href="https://cdn.example.com/files/"></head>
<body><a href="a.zip">a</a><a href="/root.zip">root</a></body></html>`
	got, err := ExtractURLs("https://example.com/page", strings.NewReader(html), ExtractOptions{
		Selector:            "a",
		DefaultAttrMappings: map[string]string{"a": "href"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://cdn.example.com/files/a.zip", "https://cdn.example.com/root.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestExtractURLs_DefaultAttrMapping_WithClassOnlySelector(t *testing.T) {
	html := `<a class="download-link" href="/doc.pdf">Download</a>`
	got, err := ExtractURLs("https://example.com", strings.NewReader(html), ExtractOptions{
		Selector:            ".download-link",
		DefaultAttrMappings: map[string]string{"a": "href"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/doc.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}
