package env

import (
	"os"
	"path/filepath"
	"reflect"
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
	cases := []struct {
		name        string
		in          string
		vars        map[string]string
		want        string
		wantMissing []string
	}{
		{
			name:        "bare_vars_preserve_missing",
			in:          "$A/$B/$C",
			vars:        map[string]string{"A": "x", "B": "y"},
			want:        "x/y/$C",
			wantMissing: []string{"C"},
		},
		{
			name:        "braced_vars",
			in:          "${A}-bar/${B}",
			vars:        map[string]string{"A": "x", "B": "y"},
			want:        "x-bar/y",
			wantMissing: []string{},
		},
		{
			name:        "escaped_dollar",
			in:          "price is $$5 for ${ITEM}",
			vars:        map[string]string{"ITEM": "pen"},
			want:        "price is $5 for pen",
			wantMissing: []string{},
		},
		{
			name:        "braced_missing_preserved",
			in:          "${MISSING}",
			vars:        map[string]string{},
			want:        "${MISSING}",
			wantMissing: []string{"MISSING"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, missing := ExpandString(c.in, c.vars)
			if got != c.want {
				t.Fatalf("unexpected expansion: got %q, want %q", got, c.want)
			}
			if !reflect.DeepEqual(missing, c.wantMissing) {
				t.Fatalf("unexpected missing vars: got %#v, want %#v", missing, c.wantMissing)
			}
		})
	}
}

func TestParseDotEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	data := `
# comment
export EXPORTED=from-export
SPACED = value with spaces
SINGLE='quoted single'
DOUBLE="quoted double"
EMPTY=
malformed
=ignored
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ParseDotEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"EXPORTED": "from-export",
		"SPACED":   "value with spaces",
		"SINGLE":   "quoted single",
		"DOUBLE":   "quoted double",
		"EMPTY":    "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected env vars: got %#v, want %#v", got, want)
	}
}
