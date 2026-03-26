package discovery

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type ExtractOptions struct {
	Selector             string
	Attr                 string
	DefaultAttrMappings  map[string]string
	RegexFilter          string
	AttrValRegexFilter   string
	SelectorMatchesLimit int
}

func ExtractURLs(baseURL string, html io.Reader, opts ExtractOptions) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(html)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}

	attr := strings.TrimSpace(opts.Attr)
	if attr == "" {
		attr = inferAttr(opts.Selector, opts.DefaultAttrMappings)
	}
	if attr == "" {
		return nil, fmt.Errorf("attr is required when selector does not map to default_attr_mappings")
	}

	var textRe *regexp.Regexp
	if strings.TrimSpace(opts.RegexFilter) != "" {
		textRe, err = regexp.Compile(opts.RegexFilter)
		if err != nil {
			return nil, fmt.Errorf("invalid regex_filter: %w", err)
		}
	}
	var urlRe *regexp.Regexp
	if strings.TrimSpace(opts.AttrValRegexFilter) != "" {
		urlRe, err = regexp.Compile(opts.AttrValRegexFilter)
		if err != nil {
			return nil, fmt.Errorf("invalid attr_val_regex_filter: %w", err)
		}
	}

	out := []string{}
	doc.Find(opts.Selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if textRe != nil && !textRe.MatchString(strings.TrimSpace(sel.Text())) {
			return true
		}
		raw, ok := sel.Attr(attr)
		if !ok || strings.TrimSpace(raw) == "" {
			return true
		}
		if urlRe != nil && !urlRe.MatchString(raw) {
			return true
		}

		u, err := url.Parse(raw)
		if err != nil {
			return true
		}
		resolved := base.ResolveReference(u)
		out = append(out, resolved.String())

		if opts.SelectorMatchesLimit > 0 && len(out) >= opts.SelectorMatchesLimit {
			return false
		}
		return true
	})

	return out, nil
}

func inferAttr(selector string, mapping map[string]string) string {
	s := strings.TrimSpace(selector)
	if s == "" {
		return ""
	}
	// Handle simple selectors: "a", "img.class", "link#id"
	tag := s
	if idx := strings.IndexAny(tag, ".#[ :>"); idx >= 0 {
		tag = tag[:idx]
	}
	if mapping == nil {
		return ""
	}
	return mapping[strings.ToLower(tag)]
}
