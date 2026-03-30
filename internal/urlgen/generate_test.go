package urlgen

import (
	"reflect"
	"strings"
	"testing"

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

func TestGenerateFile(t *testing.T) {
	task := &config.DownloadTask{
		URL:        "https://example.com/{{.URL}}",
		URLGenFile: &config.URLGenFile{VarName: "URL", File: "ignored"},
	}
	loader := func(path string) ([]string, error) {
		return []string{"a", " ", "b"}, nil
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

func TestGenerateRange_VarNameMissingInTemplate(t *testing.T) {
	task := &config.DownloadTask{
		URL:         "https://example.com/file_{{.N}}.zip",
		URLGenRange: &config.URLGenRange{VarName: "X", RangeStart: 1, RangeEnd: 1, Step: 1},
	}

	_, err := Generate(task, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must reference var_name") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}
