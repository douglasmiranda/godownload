# godownload

A CLI tool to download files from URLs. It can be used to download files given a static URL, a URL generated dynamically or URLs found in HTML pages.

## About the project

It's mostly a personal software built with AI. I am not a Go developer. It is a very useful tool so I decided to share it with the world.

I prefer to receive feedback via issues than pull requests for this project.

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


## Installation

Each release provides:

- `godownload-linux-amd64.tar.gz`
- `godownload-linux-arm64.tar.gz`
- `godownload-windows-amd64.zip`
- `checksums.txt`

Release versions increase automatically with each release workflow run, such as
`v0.0.1`, `v0.0.2`, and so on.

### Linux

Set `ARCH` to `amd64` for most Intel and AMD computers, or `arm64` for a
64-bit ARM computer:

```bash
ARCH=amd64

curl -LO "https://github.com/douglasmiranda/godownload/releases/latest/download/godownload-linux-${ARCH}.tar.gz"

curl -LO "https://github.com/douglasmiranda/godownload/releases/latest/download/checksums.txt"
sha256sum --ignore-missing --check checksums.txt

tar -xzf "godownload-linux-${ARCH}.tar.gz"
sudo install godownload /usr/local/bin/godownload
```

### Windows

Download and extract the Windows archive with PowerShell:

```powershell
Invoke-WebRequest `
  -Uri "https://github.com/douglasmiranda/godownload/releases/latest/download/godownload-windows-amd64.zip" `
  -OutFile "godownload-windows-amd64.zip"
Get-FileHash -Algorithm SHA256 "godownload-windows-amd64.zip"
Expand-Archive -Path "godownload-windows-amd64.zip" -DestinationPath "."
.\godownload.exe -config .\download.yaml
```

Move `godownload.exe` to a directory on your `PATH` to run it from anywhere.
Compare the hash with the archive's entry in `checksums.txt` on the
[latest release page](https://github.com/douglasmiranda/godownload/releases/latest).

## Build From Source

### Manual build

Go 1.26.5 or newer is required.

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

### Task targets

With [Task](https://taskfile.dev/) installed:

```bash
task -t Taskfile.dist.yaml build
task -t Taskfile.dist.yaml test
task -t Taskfile.dist.yaml e2e-tests
```

The default binary is generated at `dist/godownload`.

