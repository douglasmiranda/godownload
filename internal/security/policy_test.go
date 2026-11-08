package security

import "testing"

func TestIsURLAllowed(t *testing.T) {
	cases := []struct {
		name          string
		url           string
		allowInsecure bool
		domains       []string
		want          bool
	}{
		{"https exact domain", "https://example.com/file.zip", false, []string{"example.com"}, true},
		{"subdomain of allowed domain", "https://sub.example.com/file.zip", false, []string{"example.com"}, true},
		{"wildcard entry", "https://sub.example.com/file.zip", false, []string{"*.example.com"}, true},
		{"host with trailing dot", "https://example.com./file.zip", false, []string{"example.com"}, true},
		{"uppercase host", "https://EXAMPLE.com/file.zip", false, []string{"example.com"}, true},
		{"suffix that is not a subdomain", "https://notexample.com/file.zip", false, []string{"example.com"}, false},
		{"allowed domain as prefix of attacker domain", "https://example.com.evil.com/file.zip", false, []string{"example.com"}, false},
		{"other domain", "https://evil.com/file.zip", false, []string{"example.com"}, false},
		{"insecure http allowed", "http://example.com/file.zip", true, nil, true},
		{"insecure http blocked", "http://example.com/file.zip", false, nil, false},
		{"non-http scheme", "ftp://example.com/file.zip", true, nil, false},
		{"javascript scheme", "javascript:alert(1)", true, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsURLAllowed(tc.url, tc.allowInsecure, tc.domains); got != tc.want {
				t.Fatalf("IsURLAllowed(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestIsAllowedByExtension(t *testing.T) {
	cases := []struct {
		url     string
		allowed []string
		want    bool
	}{
		{"https://example.com/file.PDF?x=1", []string{"pdf", "zip"}, true},
		{"https://example.com/file.pdf", []string{".pdf"}, true},
		{"https://example.com/file.bin", []string{"pdf"}, false},
		{"https://example.com/download", []string{"pdf"}, false},
		{"https://example.com/", []string{"pdf"}, false},
		{"https://example.com/archive.tar.gz", []string{"tar.gz"}, true},
		{"https://example.com/archive.tar.gz", []string{"gz"}, true},
		{"https://example.com/archive.gz", []string{"tar.gz"}, false},
		{"https://example.com/pdf", []string{"pdf"}, false},
		{"https://example.com/anything", nil, true},
	}
	for _, tc := range cases {
		if got := IsAllowedByExtension(tc.url, tc.allowed); got != tc.want {
			t.Errorf("IsAllowedByExtension(%q, %v) = %v, want %v", tc.url, tc.allowed, got, tc.want)
		}
	}
}

func TestIsAllowedByContentType(t *testing.T) {
	cases := []struct {
		contentType string
		allowed     []string
		want        bool
	}{
		{"application/pdf; charset=utf-8", []string{"pdf"}, true},
		{"image/png", []string{"png"}, true},
		{"text/csv", []string{"csv"}, true},
		{"image/jpeg", []string{"jpg"}, true},
		{"application/x-zip-compressed", []string{"zip"}, true},
		{"application/gzip", []string{"tar.gz"}, true},
		{"text/plain", []string{"txt"}, true},
		{"video/mp4", []string{"mp4"}, true},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []string{"xlsx"}, true},
		{"application/octet-stream", []string{"pdf"}, true},
		{"binary/octet-stream", []string{"pdf"}, true},
		{"", []string{"pdf"}, true},
		{"text/html; charset=utf-8", []string{"pdf"}, false},
		{"application/json", []string{"pdf"}, false},
		{"application/x-pdf-tool", []string{"pdf"}, false},
		{"text/html", nil, true},
	}
	for _, tc := range cases {
		if got := IsAllowedByContentType(tc.contentType, tc.allowed); got != tc.want {
			t.Errorf("IsAllowedByContentType(%q, %v) = %v, want %v", tc.contentType, tc.allowed, got, tc.want)
		}
	}
}
