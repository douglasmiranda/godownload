# godownload

Go command-line downloader driven by YAML configuration.

The complete example catalog is in [download.yaml](download.yaml).

## What it does

- Downloads one or many files from static URLs or generated URL lists.
- Discovers links from HTML pages using CSS selectors.
- Supports retries, security restrictions, auth, metadata, and file naming templates.
- Runs multiple tasks from one config file.

## Quick start

```bash
go run ./cmd/godownload -config ./download.yaml
```

Or with the built binary:

```bash
./godownload -config ./download.yaml
```

## Config model and precedence

Top-level keys:

- `env_file`
- `env`
- `config`
- `downloads`

Rules:

- Every entry in `downloads` must define `url`.
- In `config`, you can define all task options except `url`.
- Values in a task override values from `config`, even when the task value is `false` or `0`.
- `config` also contains worker-only fields: `concurrency`, `log_level`, and `default_attr_mappings`.

Environment variable precedence:

1. Task `env`
2. Global `env`
3. `env_file` values (earlier file in list has higher precedence)
4. System environment

## Options and defaults

Defaults are split into:

- Config defaults: centralized in [internal/config/defaults.go](internal/config/defaults.go).
- Runtime behavior defaults: applied where behavior executes.

### Worker-only options (`config`)

| Option | Type | Default | Notes |
|---|---|---|---|
| `config.concurrency` | int | `10` | Number of concurrent downloads. |
| `config.log_level` | string | `INFO` | Logging verbosity. |
| `config.default_attr_mappings` | map[string]string | `a:href`, `img:src`, `link:href`, `script:src`, `iframe:src` | Used when task `attr` is not set. |

### Task options (usable in both `config` and each `downloads.<name>`)

`url` is task-only and required.

| Option | Type | Default / Effective behavior | Notes |
|---|---|---|---|
| `selector` | string | empty | If empty, task downloads/generates URLs directly from `url`/`url_gen_*`. |
| `attr` | string | empty | If empty, inferred from `default_attr_mappings` and matched element tag. |
| `regex_filter` | string | empty | Filters by matched element text. |
| `attr_val_regex_filter` | string | empty | Filters by selected attribute value (URL candidate). |
| `url_gen_range` | object | nil | Optional URL generation strategy. |
| `url_gen_list` | object | nil | Optional URL generation strategy. |
| `url_gen_file` | object | nil | Optional URL generation strategy. |
| `url_gen_dates` | object | nil | Optional URL generation strategy. |
| `force_download` | bool | `false` | If `true`, do not skip when destination exists. |
| `treat_warnings_as_errors` | bool | `false` | If `true`, runtime task warnings are promoted to errors. This can make the run fail and can trigger `stop_all_tasks.on_runtime_error`. |
| `save.base_download_dir` | string | `.` (effective) | If unset, save uses current directory at runtime. |
| `save.filename_template` | string | `{{.ORIGINAL_FILENAME}}` | Ignored when `save.filename` is set. Final filename keeps extension at the end: `save.extension` if set, otherwise original filename extension. |
| `save.filename` | string | empty | Single-file tasks only. |
| `save.override_if_exists` | bool | `false` | With `force_download=true`, overwrite existing file instead of suffixing name. |
| `save.extension` | string | empty | If set, forces/overrides file extension. |
| `save.save_metadata` | bool | `false` | Writes metadata JSON beside downloaded file. |
| `base_download_dir` | string | empty | Deprecated alias for `save.base_download_dir`. |
| `stop_all_tasks.on_runtime_error` | bool | `false` | Stop all remaining tasks when a runtime error remains after retries. |
| `stop_all_tasks.on_http_code` | []int | empty | Stop all remaining tasks when final HTTP failure code matches one in the list. |
| `timeout` | int (seconds) | `0` | `0` means runtime default HTTP timeout of `30s`. |
| `download_limit` | int | `0` (unlimited) | Limits number of downloads attempted for task. |
| `selector_matches_limit` | int | `0` (unlimited) | Limits number of selector matches converted into candidates. |
| `retry` | bool or object | disabled | `true` enables retry defaults; object config enables custom retry policy. |
| `env` | map[string]string | empty | Task-scoped env vars for template/env expansion. |
| `auth` | object | nil | Supports `basic`, `bearer`, `api_key`. |
| `security.follow_redirects` | bool | `true` (effective) | Runtime follows redirects unless explicitly false. |
| `security.allow_insecure_http` | bool | `false` | Blocks `http://` unless true. |
| `security.allow_self_signed_cert` | bool | `false` | Controls TLS verification relaxation. |
| `security.allowed_domains` | []string | empty | If set, only those domains/subdomains are allowed. |
| `security.allowed_file_types` | []string | empty | If set, restricts extension/content-type checks. |
| `max_depth` | int | `1` | Link-extraction traversal depth when using selectors. |

### Retry defaults when enabled

If retry is enabled and values are not explicitly set:

| Option | Default |
|---|---|
| `retry.on_runtime_error` | `true` |
| `retry.retries` | `3` |
| `retry.delay` | `1` |
| `retry.exponential_backoff` | `true` |
| `retry.on_http_code` | empty (runtime defaults to 4xx and 5xx) |

## Notes on future default changes

- Change config-level defaults in [internal/config/defaults.go](internal/config/defaults.go).
- Keep runtime behavior defaults aligned when needed:
  - HTTP timeout and redirect behavior in [internal/downloader/downloader.go](internal/downloader/downloader.go).
  - Final fallback filename template and save directory behavior in [internal/storage/storage.go](internal/storage/storage.go).

This split is intentional: config loading decides inherited values, runtime layers decide operational fallbacks.

## Examples

See [download.yaml](download.yaml) for comprehensive examples and inline guidance.
