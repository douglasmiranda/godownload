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

func TestRedactText_BasicUserInfo(t *testing.T) {
	in := "https://user:pass@example.com/file.zip"
	out := RedactText(in)
	if strings.Contains(out, "pass") {
		t.Fatalf("expected password redacted: %s", out)
	}
}
