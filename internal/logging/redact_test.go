package logging

import (
	"strings"
	"testing"
)

func TestRedactText_URLQuery(t *testing.T) {
	in := "failed URL https://example.com/file.pdf?token=abc123&x=1"
	out := RedactText(in)
	if strings.Contains(out, "abc123") {
		t.Fatalf("expected token to be redacted: %s", out)
	}
	if !strings.Contains(out, "token=%5BREDACTED%5D") {
		t.Fatalf("expected encoded token marker in output: %s", out)
	}
}

func TestRedactText_Bearer(t *testing.T) {
	in := "Authorization: Bearer very-secret-token"
	out := RedactText(in)
	if !strings.Contains(out, "Bearer [REDACTED]") {
		t.Fatalf("expected bearer token redacted: %s", out)
	}
}

func TestNewRedactor_RemovesConfiguredSecrets(t *testing.T) {
	redact := NewRedactor([]string{"s3cr3t value", "ab", ""})
	cases := []string{
		"hook output: s3cr3t value",
		"https://example.com/file?k=s3cr3t+value",
		"https://example.com/s3cr3t%20value/file",
	}
	for _, in := range cases {
		out := redact(in)
		if strings.Contains(out, "s3cr3t") {
			t.Errorf("redact(%q) = %q, secret still present", in, out)
		}
	}
	if got := redact("about tabs"); got != "about tabs" {
		t.Errorf("short secrets must not be replaced, got %q", got)
	}
	if got := redact("token=abc123"); strings.Contains(got, "abc123") {
		t.Errorf("pattern-based redaction must still apply, got %q", got)
	}

	prefixed := NewRedactor([]string{"abcd", "abcdefgh"})
	if got := prefixed("value abcdefgh"); got != "value [REDACTED]" {
		t.Errorf("secret sharing a prefix with another must be fully replaced, got %q", got)
	}
}

func TestRedactText_BasicUserInfo(t *testing.T) {
	in := "https://user:pass@example.com/file.zip"
	out := RedactText(in)
	if strings.Contains(out, "pass") {
		t.Fatalf("expected password redacted: %s", out)
	}
}
