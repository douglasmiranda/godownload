package logging

import (
	"net/url"
	"regexp"
	"slices"
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

// minSecretLength avoids replacing short values that would match ordinary
// text, such as a one-character password.
const minSecretLength = 4

// NewRedactor returns a function that removes the given secret values, in
// plain and URL-encoded form, before applying RedactText.
func NewRedactor(secrets []string) func(string) string {
	var values []string
	for _, secret := range secrets {
		for _, v := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			if len(v) >= minSecretLength && !slices.Contains(values, v) {
				values = append(values, v)
			}
		}
	}
	if len(values) == 0 {
		return RedactText
	}
	// Longer values first, so a secret that starts with another one is
	// replaced completely.
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	replacements := make([]string, 0, len(values)*2)
	for _, v := range values {
		replacements = append(replacements, v, "[REDACTED]")
	}
	replacer := strings.NewReplacer(replacements...)
	return func(s string) string {
		return RedactText(replacer.Replace(s))
	}
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
