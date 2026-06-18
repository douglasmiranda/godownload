package security

import "testing"

func TestIsURLAllowed(t *testing.T) {
	if !IsURLAllowed("https://example.com/file.zip", false, []string{"example.com"}) {
		t.Fatal("expected https example.com to be allowed")
	}
	if IsURLAllowed("http://example.com/file.zip", false, nil) {
		t.Fatal("expected insecure http to be blocked")
	}
	if IsURLAllowed("https://evil.com/file.zip", false, []string{"example.com"}) {
		t.Fatal("expected domain restriction to block")
	}
}

func TestIsAllowedByExtension(t *testing.T) {
	if !IsAllowedByExtension("https://example.com/file.PDF?x=1", []string{"pdf", "zip"}) {
		t.Fatal("expected extension to be allowed")
	}
	if IsAllowedByExtension("https://example.com/file.bin", []string{"pdf"}) {
		t.Fatal("expected extension to be blocked")
	}
}

func TestIsAllowedByContentType(t *testing.T) {
	if !IsAllowedByContentType("application/pdf; charset=utf-8", []string{"pdf"}) {
		t.Fatal("expected content-type to be allowed")
	}
	if IsAllowedByContentType("application/octet-stream", []string{"pdf"}) {
		t.Fatal("expected content-type to be blocked")
	}
	if IsAllowedByContentType("application/x-pdf-tool", []string{"pdf"}) {
		t.Fatal("expected substring content-type match to be blocked")
	}
}
