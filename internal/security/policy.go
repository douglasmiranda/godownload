package security

import (
	"mime"
	"net/url"
	"path"
	"strings"
)

func IsURLAllowed(rawURL string, allowInsecureHTTP bool, allowedDomains []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "http" && !allowInsecureHTTP {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if len(allowedDomains) == 0 {
		return true
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range allowedDomains {
		norm := strings.ToLower(strings.TrimSpace(d))
		if norm == "" {
			continue
		}
		if host == norm || strings.HasSuffix(host, "."+norm) {
			return true
		}
	}
	return false
}

func IsAllowedByExtension(rawURL string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), ".")
	if ext == "" {
		return false
	}
	for _, a := range allowed {
		if ext == strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), ".") {
			return true
		}
	}
	return false
}

func IsAllowedByContentType(contentType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.ToLower(strings.Split(contentType, ";")[0]))
	}
	for _, a := range allowed {
		norm := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), ".")
		if norm == "" {
			continue
		}
		if strings.EqualFold(mediaType, "application/"+norm) || strings.EqualFold(mediaType, "image/"+norm) || strings.EqualFold(mediaType, "text/"+norm) {
			return true
		}
		if strings.Contains(strings.ToLower(mediaType), norm) {
			return true
		}
	}
	return false
}
