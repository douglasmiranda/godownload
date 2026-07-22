# godownload

CLI tool to download files from static URLs, HTML pages (via selectors), or generated URL sets.

## Status

Installation is not released yet.

## Installation

TODO

## Build From Source

### Manual build

```bash
go build -trimpath -ldflags="-s -w" -o dist/godownload ./cmd/godownload
```

Run:

```bash
./dist/godownload -config ./download.yaml
```

Run only specific tasks from the config:

```bash
./dist/godownload -config ./download.yaml -tasks task1
./dist/godownload -config ./download.yaml -tasks task1,task2,task3
```

Task names are comma-separated. Spaces are allowed when the value is quoted, for example
`-tasks "task1, task2, task3"`. The complete config is loaded and validated before
the selected tasks run, and an unknown task name stops the run.

### Make targets

```bash
make build
make test
make e2e-tests
```

The default binary is generated at `dist/godownload`.

## Config Examples

### 1) Simple: one URL

```yaml
downloads:
  one_file:
    url: https://example.com/file.zip
```

### 2) Sophisticated: selector-based extraction

```yaml
config:
  save:
    base_download_dir: ./downloads

downloads:
  page_assets:
    url: https://example.com/downloads
    selector: a.download-link
    regex_filter: "Release \\d+"
    save:
      filename_template: "{{.DATE}}_{{.ORIGINAL_FILENAME}}"
```

### 3) Advanced: URL generation + retry + security + regex filters

```yaml
config:
  save:
    base_download_dir: ./downloads

downloads:
  generated_reports:
    url: https://example.com/reports/{{.DATE}}/index.html
    url_gen_dates:
      var_name: DATE
      date_range_start: "2026-01-01"
      date_range_end: "2026-01-31"
      date_format: YYYY-MM-DD

    selector: a.report-link
    regex_filter: "Monthly Report"
    attr_val_regex_filter: "\\.pdf$"

    retry:
      on_runtime_error: true
      on_http_code: [429, 500, 502, 503, 504]
      retries: 5
      delay: 2
      exponential_backoff: true

    security:
      allow_insecure_http: false
      follow_redirects: true
      allowed_domains: [example.com, cdn.example.com]
      allowed_file_types: [pdf]

    save:
      filename_template: "{{.DOMAIN}}_{{.DATE}}_{{.ORIGINAL_FILENAME}}"
      save_metadata: true
```

## Full Documentation

For complete option reference, defaults, interactions, and validation behavior, see [DOWNLOAD.md](DOWNLOAD.md).
