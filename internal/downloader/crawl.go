package downloader

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/discovery"
	"github.com/douglasmiranda/godownload/internal/security"
)

// maxPageBytes bounds how much of a selector page is read into memory.
const maxPageBytes = 64 << 20

// maxCrawlPages bounds how many discovered pages (beyond the task URLs) one
// task reads when max_depth > 1, since each level can multiply the number of
// pages.
const maxCrawlPages = 1000

var errNotHTML = errors.New("response is not an HTML page")

type crawlResult struct {
	candidates []string
	errors     []error
	warnings   []string
	// blocked counts discovered links skipped by the security policy.
	blocked int
	// abort is set when an error matched stop_all_tasks.
	abort error
}

// resolveDownloadCandidates returns the URLs to download. Without a selector
// these are the seed URLs. With a selector, seed pages are fetched and the
// matching links become candidates; when max_depth > 1, matching links that
// turn out to be HTML pages are crawled for further links instead of being
// downloaded.
func (s *Service) resolveDownloadCandidates(ctx context.Context, client *taskClient, seedURLs []string, task *config.DownloadTask, defaultAttrMapping map[string]string, concurrency int) crawlResult {
	if strings.TrimSpace(task.Selector) == "" {
		return crawlResult{candidates: seedURLs}
	}
	opts := discovery.ExtractOptions{
		Selector:             task.Selector,
		Attr:                 task.Attr,
		DefaultAttrMappings:  defaultAttrMapping,
		RegexFilter:          task.RegexFilter,
		AttrValRegexFilter:   task.AttrValRegexFilter,
		SelectorMatchesLimit: task.SelectorMatchesLimit,
	}
	maxDepth := max(task.MaxDepth, 1)

	var res crawlResult
	addCandidate := func(u string) {
		if !security.IsAllowedByExtension(u, task.Security.AllowedFileTypes) {
			res.blocked++
			s.debugf("skipping discovered link blocked by security.allowed_file_types: %s", u)
			return
		}
		res.candidates = append(res.candidates, u)
	}
	visited := map[string]struct{}{}
	for _, u := range seedURLs {
		visited[u] = struct{}{}
	}
	frontier := seedURLs
	crawled := 0

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			res.abort = err
			return res
		}
		// Candidates are downloaded in discovery order, so once download_limit
		// is reached deeper pages cannot change which files are downloaded.
		if depth > 1 && task.DownloadLimit > 0 && len(res.candidates) >= task.DownloadLimit {
			break
		}
		if depth > 1 {
			if remaining := maxCrawlPages - crawled; len(frontier) > remaining {
				res.warnings = append(res.warnings, fmt.Sprintf("crawl stopped after reading %d linked pages; %d more were not read (narrow the selector, lower max_depth or set download_limit)", maxCrawlPages, len(frontier)-remaining))
				frontier = frontier[:remaining]
			}
			crawled += len(frontier)
			if len(frontier) == 0 {
				break
			}
		}
		pages := s.fetchPages(ctx, client, frontier, depth > 1, opts, concurrency)

		var next []string
		for i, page := range pages {
			pageURL := frontier[i]
			if errors.Is(page.err, errNotHTML) {
				addCandidate(pageURL)
				continue
			}
			if page.err != nil {
				err := fmt.Errorf("selector page %s: %w", pageURL, page.err)
				res.errors = append(res.errors, err)
				if shouldStopAllTasks(task, err) {
					res.abort = err
					return res
				}
				continue
			}
			for _, link := range page.links {
				if !security.IsURLAllowed(link, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
					res.blocked++
					s.debugf("skipping discovered link blocked by security policy: %s", link)
					continue
				}
				if _, seen := visited[link]; seen {
					continue
				}
				visited[link] = struct{}{}
				if depth < maxDepth && mayBeHTMLPage(link) {
					next = append(next, link)
				} else {
					addCandidate(link)
				}
			}
		}
		frontier = next
	}
	return res
}

// pageExtensions are URL path extensions usually served as HTML pages.
var pageExtensions = map[string]bool{
	"": true, ".html": true, ".htm": true, ".xhtml": true, ".shtml": true,
	".php": true, ".asp": true, ".aspx": true, ".jsp": true, ".cgi": true, ".cfm": true,
}

// mayBeHTMLPage reports whether a discovered link could be an HTML page worth
// crawling. Links whose extension has a known non-HTML type, such as .pdf or
// .zip, are files, so they are downloaded without first being fetched as
// pages. Unknown extensions are fetched and checked.
func mayBeHTMLPage(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if pageExtensions[ext] {
		return true
	}
	types := security.KnownMediaTypes(ext)
	if len(types) == 0 {
		return true
	}
	return slices.ContainsFunc(types, isHTMLMediaType)
}

func isHTMLMediaType(mediaType string) bool {
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

type pageResult struct {
	links []string
	err   error
}

// fetchPages fetches and parses pages concurrently, returning results in the
// order of urls. When requireHTML is set, non-HTML responses are not read and
// report errNotHTML.
func (s *Service) fetchPages(ctx context.Context, client *taskClient, urls []string, requireHTML bool, opts discovery.ExtractOptions, concurrency int) []pageResult {
	results := make([]pageResult, len(urls))
	sem := make(chan struct{}, max(concurrency, 1))
	var wg sync.WaitGroup
	for i, u := range urls {
		select {
		case <-ctx.Done():
			results[i].err = ctx.Err()
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			links, err := s.fetchPage(ctx, client, u, requireHTML, opts)
			results[i] = pageResult{links: links, err: err}
		}()
	}
	wg.Wait()
	return results
}

func (s *Service) fetchPage(ctx context.Context, client *taskClient, pageURL string, requireHTML bool, opts discovery.ExtractOptions) ([]string, error) {
	var links []string
	err := client.get(ctx, pageURL, func(resp *http.Response, body io.Reader) error {
		br := bufio.NewReader(body)
		if requireHTML && !looksLikeHTML(resp.Header.Get("Content-Type"), br) {
			return errNotHTML
		}
		if resp.ContentLength > maxPageBytes {
			return fmt.Errorf("page is larger than %d bytes", maxPageBytes)
		}
		data, err := io.ReadAll(newLimitReader(br, maxPageBytes, fmt.Errorf("page is larger than %d bytes", maxPageBytes)))
		if err != nil {
			return err
		}
		// Resolve relative links against the final URL after redirects.
		links, err = discovery.ExtractURLs(resp.Request.URL.String(), bytes.NewReader(data), opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	return links, nil
}

func looksLikeHTML(contentType string, br *bufio.Reader) bool {
	if strings.TrimSpace(contentType) == "" {
		peek, _ := br.Peek(512)
		contentType = http.DetectContentType(peek)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return isHTMLMediaType(mediaType)
}
