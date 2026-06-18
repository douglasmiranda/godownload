package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFiles_FirstWins(t *testing.T) {
	dir := t.TempDir()
	high := filepath.Join(dir, ".env.local")
	low := filepath.Join(dir, ".env")

	if err := os.WriteFile(high, []byte("A=1\nB=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(low, []byte("A=9\nC=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadEnvFiles([]string{".env.local", ".env"}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["A"] != "1" {
		t.Fatalf("expected A=1, got %s", got["A"])
	}
	if got["B"] != "2" || got["C"] != "3" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestExpandString(t *testing.T) {
	got, missing := ExpandString("$A/$B/$C", map[string]string{"A": "x", "B": "y"})
	if got != "x/y/$C" {
		t.Fatalf("unexpected expansion: %s", got)
	}
	if len(missing) != 1 || missing[0] != "C" {
		t.Fatalf("unexpected missing vars: %#v", missing)
	}
}

func TestExpandString_BracedAndEscaped(t *testing.T) {
	got, missing := ExpandString("${A}-bar/${B}", map[string]string{"A": "x", "B": "y"})
	if got != "x-bar/y" {
		t.Fatalf("unexpected braced expansion: %q", got)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing vars: %#v", missing)
	}

	got, missing = ExpandString("price is $$5 for ${ITEM}", map[string]string{"ITEM": "pen"})
	if got != "price is $5 for pen" {
		t.Fatalf("unexpected escaped expansion: %q", got)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing vars: %#v", missing)
	}

	got, missing = ExpandString("${MISSING}", map[string]string{})
	if got != "${MISSING}" {
		t.Fatalf("expected unresolved braced token to be preserved, got %q", got)
	}
	if len(missing) != 1 || missing[0] != "MISSING" {
		t.Fatalf("unexpected missing vars: %#v", missing)
	}
}
