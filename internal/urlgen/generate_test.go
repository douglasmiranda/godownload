package urlgen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestGenerateRange(t *testing.T) {
	task := &config.DownloadTask{
		URL:         "https://example.com/file_{{.N}}.zip",
		URLGenRange: &config.URLGenRange{VarName: "N", RangeStart: 1, RangeEnd: 3, Step: 1},
	}
	got, err := Generate(task, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://example.com/file_1.zip",
		"https://example.com/file_2.zip",
		"https://example.com/file_3.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestGenerateList(t *testing.T) {
	task := &config.DownloadTask{
		URL:        "https://example.com/file_{{.NAME}}.zip",
		URLGenList: &config.URLGenList{VarName: "NAME", Values: []string{"a", "b"}},
	}
	got, err := Generate(task, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/file_a.zip", "https://example.com/file_b.zip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestGenerateDates(t *testing.T) {
	task := &config.DownloadTask{
		URL: "https://example.com/file_{{.DATE}}.zip",
		URLGenDates: &config.URLGenDates{
			VarName: "DATE", DateRangeStart: "2022-01-01", DateRangeEnd: "2022-01-03", DateFormat: "YYYY-MM-DD",
		},
	}
	got, err := Generate(task, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://example.com/file_2022-01-01.zip",
		"https://example.com/file_2022-01-02.zip",
		"https://example.com/file_2022-01-03.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestGenerateDates_DefaultsToYYYYMMDDWhenFormatOmitted(t *testing.T) {
	task := &config.DownloadTask{
		URL: "https://example.com/file_{{.DATE}}.zip",
		URLGenDates: &config.URLGenDates{
			VarName: "DATE", DateRangeStart: "2022-01-01", DateRangeEnd: "2022-01-02",
		},
	}
	got, err := Generate(task, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"https://example.com/file_2022-01-01.zip",
		"https://example.com/file_2022-01-02.zip",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestGenerateFile(t *testing.T) {
	task := &config.DownloadTask{
		URL:        "https://example.com/{{.URL}}",
		URLGenFile: &config.URLGenFile{VarName: "URL", File: "ignored"},
	}
	loader := func(path string) ([]string, error) {
		return []string{"# list of files", "a", " ", "  # indented comment", "b"}, nil
	}
	got, err := Generate(task, loader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/a", "https://example.com/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected URLs: %#v", got)
	}
}

func TestGenerateRange_NegativeStepFailsInsteadOfLooping(t *testing.T) {
	task := &config.DownloadTask{
		URL:         "https://example.com/file_{{.N}}.zip",
		URLGenRange: &config.URLGenRange{VarName: "N", RangeStart: 1, RangeEnd: 10, Step: -1},
	}

	done := make(chan struct{})
	var got []string
	var err error
	go func() {
		got, err = Generate(task, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not return; negative step likely caused an infinite loop")
	}

	if err == nil {
		t.Fatalf("expected error for negative step, got %#v", got)
	}
	if !strings.Contains(err.Error(), "step") {
		t.Fatalf("expected step error, got %v", err)
	}
}

func TestLocalFileLoader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "urls.txt")
	if err := os.WriteFile(path, []byte("https://example.com/a\n\nhttps://example.com/b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LocalFileLoader(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/a", "", "https://example.com/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected lines: %#v", got)
	}
}

func TestReadLines_BOMCRLFAndLongLines(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("x", 100_000)
	input := "\ufeffhttps://example.com/a\r\n" + long + "\r\nhttps://example.com/last"

	got, err := ReadLines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"https://example.com/a", long, "https://example.com/last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected lines: %q", got)
	}
}

func TestLocalFileLoader_MissingFile(t *testing.T) {
	_, err := LocalFileLoader(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
