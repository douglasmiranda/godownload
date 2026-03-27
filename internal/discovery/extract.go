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
	if attr == "" && len(opts.DefaultAttrMappings) == 0 {
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
	sawSelection := false
	sawMappedAttr := false
	doc.Find(opts.Selector).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		sawSelection = true

		effectiveAttr := attr
		if effectiveAttr == "" {
			effectiveAttr = inferAttrFromSelection(sel, opts.DefaultAttrMappings)
		}
		if effectiveAttr == "" {
			return true
		}
		sawMappedAttr = true

		if textRe != nil && !textRe.MatchString(strings.TrimSpace(sel.Text())) {
			return true
		}
		raw, ok := sel.Attr(effectiveAttr)
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

	if strings.TrimSpace(opts.Attr) == "" && sawSelection && !sawMappedAttr {
		return nil, fmt.Errorf("attr is required when selector does not map to default_attr_mappings")
	}

	return out, nil
}

func inferAttr(selector string, mapping map[string]string) string {
	s := strings.TrimSpace(selector)
	if s == "" {
		return ""
	}
	// Handle selector groups and combinators by focusing on the final simple selector.
	group := s
	if idx := strings.Index(group, ","); idx >= 0 {
		group = group[:idx]
	}
	parts := strings.FieldsFunc(group, func(r rune) bool {
		switch r {
		case ' ', '>', '+', '~':
			return true
		default:
			return false
		}
	})
	tag := strings.TrimSpace(group)
	if len(parts) > 0 {
		tag = strings.TrimSpace(parts[len(parts)-1])
	}
	if idx := strings.IndexAny(tag, ".#[:"); idx >= 0 {
		tag = tag[:idx]
	}
	if idx := strings.LastIndex(tag, "|"); idx >= 0 {
		tag = tag[idx+1:]
	}
	tag = strings.TrimSpace(tag)
	if mapping == nil {
		return ""
	}
	return mapping[strings.ToLower(tag)]
}

func inferAttrFromSelection(sel *goquery.Selection, mapping map[string]string) string {
	if mapping == nil {
		return ""
	}
	tag := strings.ToLower(goquery.NodeName(sel))
	if tag == "" {
		return ""
	}
	return mapping[tag]
}
