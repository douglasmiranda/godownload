package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasmiranda/godownload/internal/config"
)

// FuzzResolveTargetStaysInBaseDir checks that URLs found on web pages can
// never resolve outside save.base_download_dir. Templates come from the
// user's own config, so a fixed set covering every URL-derived variable is
// used instead of fuzzing them.
func FuzzResolveTargetStaysInBaseDir(f *testing.F) {
	seeds := []struct{ urlPath, filename string }{
		{"files/report.pdf", ""},
		{"safe/%2e%2e/%2e%2e/etc/passwd", ""},
		{"a/b/../../../c.txt", ""},
		{`..\..\x.txt`, ""},
		{"C:/Windows/x.txt", ""},
		{"%2F%2Fetc/passwd", ""},
		{"%00/%2F..%2F/x", ""},
		{"CON/aux.txt", ""},
		{"x.txt", "../../etc/passwd"},
		{"x.txt", `..\..\boot.ini`},
	}
	for _, s := range seeds {
		f.Add(s.urlPath, s.filename)
	}

	templates := []string{
		"",
		"{{.URL_PATH}}",
		"{{.URL_DIR}}/{{.ORIGINAL_FILENAME}}",
		"{{.DOMAIN}}/{{.ORIGINAL_FILENAME}}",
		"{{.URL}}",
		"{{.DATE_DIR}}/{{.ORIGINAL_FILENAME}}",
	}
	base := f.TempDir()
	f.Fuzz(func(t *testing.T, urlPath, filename string) {
		for _, tpl := range templates {
			task := &config.DownloadTask{
				Save: config.SaveConfig{
					BaseDownloadDir:  base,
					FilenameTemplate: tpl,
					Filename:         filename,
				},
			}
			target, err := ResolveTarget(task, "https://example.com/"+urlPath)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(base, target.Path())
			if err != nil {
				t.Fatalf("template %q: target %q is not relative to base: %v", tpl, target.Path(), err)
			}
			if rel == "." || filepath.IsAbs(rel) {
				t.Fatalf("template %q: target %q does not name a file inside base", tpl, target.Path())
			}
			for _, part := range strings.Split(rel, string(filepath.Separator)) {
				if part == "" || part == "." || part == ".." || strings.ContainsRune(part, 0) {
					t.Fatalf("template %q: target %q has an invalid component %q", tpl, target.Path(), part)
				}
			}
			if filepath.Join(base, target.Name()) != target.Path() {
				t.Fatalf("template %q: Name %q and Path %q disagree", tpl, target.Name(), target.Path())
			}
		}
	})
}
