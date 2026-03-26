package logging

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	urlPattern        = regexp.MustCompile(`https?://[^\s]+`)
	bearerPattern     = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-~+/]+=*`)
	apiKeyPattern     = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)=([^\s&]+)`)
	sensitiveQueryKey = map[string]struct{}{
		"api_key":  {},
		"apikey":   {},
		"token":    {},
		"password": {},
		"secret":   {},
		"auth":     {},
		"key":      {},
	}
)

func RedactText(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}

	out := bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	out = apiKeyPattern.ReplaceAllString(out, "$1=[REDACTED]")
	out = urlPattern.ReplaceAllStringFunc(out, redactURLString)
	return out
}

func redactURLString(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		username := u.User.Username()
		if username != "" {
			u.User = url.UserPassword(username, "[REDACTED]")
		} else {
			u.User = url.User("[REDACTED]")
		}
	}
	q := u.Query()
	changed := false
	for k := range q {
		if isSensitiveQueryKey(k) {
			q.Set(k, "[REDACTED]")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func isSensitiveQueryKey(k string) bool {
	_, ok := sensitiveQueryKey[strings.ToLower(strings.TrimSpace(k))]
	return ok
}
