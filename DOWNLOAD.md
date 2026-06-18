# GODOWNLOAD YAML REFERENCE

This document is the reference for Godownload YAML.

**The source of truth is always the code**, but this doc tries to be comprehensive and explicit about all features, options, and behaviors.

Goals:
- Complete feature coverage.
- Table-first option docs.
- Situation-focused task guidance.
- Explicit defaults and implicit behavior.

## Quick Reference

| Goal | Start here | Core options |
|---|---|---|
| Download one known file | [1) Static Single URL Download](#1-static-single-url-download) | `url`, `save.*`, `force_download` |
| Extract links from an HTML page | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) | `selector`, `attr`, `regex_filter`, `attr_val_regex_filter` |
| Build URLs from pattern/list/date/file | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) | `url_gen_*`, templated `url` |
| Handle flaky endpoints | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) | `retry.*`, `timeout`, `stop_all_tasks.*` |
| Enforce security policy | [5) Authentication and Security](#5-authentication-and-security) | `auth.*`, `security.*` |
| Control naming and collisions | [6) File Save and Naming](#6-file-save-and-naming) | `save.*`, `force_download`, `save.override_if_exists` |
| See every option in one place | [Option Coverage Checklist](#option-coverage-checklist) | all canonical field paths |

## Recipes Index

| Goal | Start with |
|---|---|
| Download one known file | [1) Static Single URL Download](#1-static-single-url-download) |
| Parse a page and fetch matching links | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |
| Build URLs from IDs, dates, or files | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) |
| Improve resilience for flaky endpoints | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| Restrict network surface and file types | [5) Authentication and Security](#5-authentication-and-security) |
| Control naming and overwrite behavior | [6) File Save and Naming](#6-file-save-and-naming) |


## Table Conventions

| Column | Meaning |
|---|---|
| Option | YAML field path |
| Type | YAML type |
| Default | Built-in default, or `none` |
| Required (Req.) | `Yes`, `No`, or `Conditional` |
| Notes | Behavior, constraints, and interactions |

## YAML Layout

```yaml
env_file: []
env: {}
config: {}
downloads:
  task_name: {}
```

## Configuration Model

### Environment Variable Precedence

Highest to lowest precedence during task resolution:

| Priority | Source |
|---|---|
| 1 | `downloads.<task>.env` |
| 2 | `config.env` |
| 3 | root `env` |
| 4 | `env_file` files (earlier list entries override later ones) |
| 5 | system environment |

### Environment Expansion Rules

| Topic | Behavior |
|---|---|
| Variable syntax | Use `$VAR_NAME` or `${VAR_NAME}` in strings. |
| Missing variable | Validation diagnostic is produced and token stays unresolved. |
| `env_file` paths | Relative values resolve from YAML config directory. Missing files are ignored. |

### Root Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `env_file` | `[]string` | `[]` | No | Dotenv file list. Parsed so first entry has highest precedence. |
| `env` | `map[string]string` | `{}` | No | Root env vars for interpolation. Lower precedence than `config.env` and task `env`. |
| `config` | `object` | built-in defaults | No | Contains global worker settings and global task defaults. |
| `downloads` | `map[string]task` | none | Yes | Task definitions keyed by task name. |

## Global Configuration (`config`)

Everything under `config` is global: worker-level settings and default values inherited by tasks.

### Worker Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `task_concurrency` | `int` | `5` | No | Number of tasks processed concurrently. Must be `> 0`. |
| `downloads_per_task_concurrency` | `int` | `3` | No | Default per-task download concurrency. Must be `> 0`. |
| `log_level` | `string` | `INFO` | No | Worker log level. |

### Global Default Task Options

All options below are optional. If set here, tasks inherit them unless task-level values override them.

| Option | Type | Default | Notes |
|---|---|---|---|
| `selector` | `string` | `""` | CSS selector used to extract links from HTML pages. |
| `attr` | `string` | `""` | Attribute read from matching elements. If empty, inferred from `default_attr_mappings`. |
| `regex_filter` | `string` | `""` | Regex applied to element text. |
| `attr_val_regex_filter` | `string` | `""` | Regex applied to extracted attribute value. |
| `url_gen_range` | `object` | none | Dynamic URL generation by numeric range. Mutually exclusive with other `url_gen_*`. |
| `url_gen_list` | `object` | none | Dynamic URL generation by literal list. Mutually exclusive with other `url_gen_*`. |
| `url_gen_file` | `object` | none | Dynamic URL generation by values loaded from local file or URL. Mutually exclusive with other `url_gen_*`. |
| `url_gen_dates` | `object` | none | Dynamic URL generation by date range. Mutually exclusive with other `url_gen_*`. |
| `force_download` | `bool` | `false` | If `false`, existing files are skipped. |
| `treat_warnings_as_errors` | `bool` | `false` | Promotes warnings to task errors. |
| `stop_all_tasks.on_runtime_error` | `bool` | `false` | Marks abort-all when runtime error happens. |
| `stop_all_tasks.on_http_code` | `[]int` | `[]` | Marks abort-all for matching HTTP codes after retries finish. |
| `timeout` | `int` | `0` | Request timeout seconds. Must be `>= 0`. |
| `download_limit` | `int` | `0` | Max candidate URLs downloaded per task (`0` means unlimited). |
| `selector_matches_limit` | `int` | `0` | Max selector matches parsed per page (`0` means unlimited). |
| `retry` | `bool` or `object` | `false` | Retry policy. `true` enables defaults. |
| `env` | `map[string]string` | `{}` | Global task-default env vars. |
| `auth` | `object` | none | Global authentication defaults. |
| `security` | `object` | see section | Global security defaults. |
| `max_depth` | `int` | `1` | Selector crawl depth. Must be `>= 1`. |
| `save.base_download_dir` | `string` | `.` | Base output directory. |
| `save.filename_template` | `string` | `{{.ORIGINAL_FILENAME}}` | Used when `save.filename` is empty. |
| `save.filename` | `string` | `""` | Fixed output filename for single-URL tasks only. |
| `save.override_if_exists` | `bool` | `false` | Used only when `force_download: true`. |
| `save.extension` | `string` | `""` | Extension override. |
| `save.save_metadata` | `bool` | `false` | Writes `<filename>_metadata.json` beside downloaded file. |
| `base_download_dir` | `string` | `""` | Deprecated. Use `save.base_download_dir`. |

### `default_attr_mappings`

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `config.default_attr_mappings` | `map[string]string` | built-in map below | No | Used when task `attr` is empty. |

Built-in mapping when no mapping is configured:

| HTML tag | Attribute |
|---|---|
| `a` | `href` |
| `img` | `src` |
| `link` | `href` |
| `script` | `src` |
| `iframe` | `src` |

## Download Tasks (`downloads`)

### Task Skeleton

```yaml
downloads:
  my_task:
    url: https://example.com/file.zip
```

### Core Task Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `downloads.<name>.url` | `string` | none | Yes | Can be direct URL, HTML page URL, or templated URL for `url_gen_*`. |
| `downloads.<name>.env` | `map[string]string` | `{}` | No | Highest environment precedence. |

## Download Tasks by Situation

### 1) Static Single URL Download

Use this when you already know the file URL.

#### Option Table

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `url` | `string` | none | Yes | Must resolve to `http` or `https`. |
| `timeout` | `int` | `0` | No | Request timeout seconds. |
| `force_download` | `bool` | `false` | No | If file exists and false, file is skipped. |
| `save.*` | `object` | inherited | No | Naming and output options. |

Minimal example:

```yaml
downloads:
  simple_file:
    url: https://example.com/archive.zip
```

Advanced example:

```yaml
downloads:
  simple_file_controlled:
    url: https://example.com/archive.zip
    timeout: 20
    force_download: true
    save:
      base_download_dir: ./downloads
      filename: archive-latest.zip
      override_if_exists: true
      save_metadata: true
```

See also: [6) File Save and Naming](#6-file-save-and-naming), [4) Reliability and Fail-fast](#4-reliability-and-fail-fast).

### 2) Discover Links from HTML (Selector)

Use this when URL points to a page that contains download links.

#### Option Table

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `selector` | `string` | `""` | No | CSS selector for elements containing URLs. |
| `attr` | `string` | inferred | No | If empty, inferred from `default_attr_mappings`. |
| `regex_filter` | `string` | `""` | No | Regex against element text. Must compile. |
| `attr_val_regex_filter` | `string` | `""` | No | Regex against extracted attribute value. Must compile. |
| `selector_matches_limit` | `int` | `0` | No | Stop extraction after N selector matches per page. |
| `max_depth` | `int` | `1` | No | Recursive crawl depth when following discovered links. |

Minimal example:

```yaml
downloads:
  links_on_page:
    url: https://example.com/downloads
    selector: a
```

Advanced example:

```yaml
downloads:
  filtered_assets:
    url: https://example.com/downloads
    selector: a.asset-link
    attr: data-download-url
    regex_filter: "Report \\d{4}"
    attr_val_regex_filter: "\\.(pdf|zip)$"
    selector_matches_limit: 50
    max_depth: 2
```

See also: [Validation Rules and Failure Modes](#validation-rules-and-failure-modes), [Selector and URL Candidate Limits](#selector-and-url-candidate-limits).

### 3) Generate URLs Dynamically

Use this when URLs come from patterns, lists, files, or date ranges.

#### Generator Selector Table

| Generator | Use when | Required fields |
|---|---|---|
| `url_gen_range` | Sequential numeric values | `var_name`, `range_start`, `range_end` |
| `url_gen_list` | Explicit finite values | `var_name`, `values` |
| `url_gen_file` | Values from local/remote file | `var_name`, `file` |
| `url_gen_dates` | Date-based URL patterns | `var_name`, `date_range_start`, `date_range_end` |

#### `url_gen_range`

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `url_gen_range.var_name` | `string` | none | Yes | Must be referenced in `url` as `{{.VAR_NAME}}`. |
| `url_gen_range.range_start` | `int` | none | Yes | Start inclusive. |
| `url_gen_range.range_end` | `int` | none | Yes | End inclusive, must be `>= range_start`. |
| `url_gen_range.step` | `int` | `1` | No | Must not be `0`. |

#### `url_gen_list`

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `url_gen_list.var_name` | `string` | none | Yes | Must be referenced in `url` as `{{.VAR_NAME}}`. |
| `url_gen_list.values` | `[]string` | none | Yes | Must contain at least one value. |

#### `url_gen_file`

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `url_gen_file.var_name` | `string` | none | Yes | Must be referenced in `url` as `{{.VAR_NAME}}`. |
| `url_gen_file.file` | `string` | none | Yes | Local file path or URL. Relative paths resolve from config directory. |

#### `url_gen_dates`

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `url_gen_dates.var_name` | `string` | none | Yes | Must be referenced in `url` as `{{.VAR_NAME}}`. |
| `url_gen_dates.date_range_start` | `string` | none | Yes | Must match `date_format`. |
| `url_gen_dates.date_range_end` | `string` | none | Yes | Must match `date_format` and be `>= start`. |
| `url_gen_dates.date_format` | `string` | `YYYY-MM-DD` | No | Allowed: `YYYY-MM-DD`, `MM/DD/YYYY`, `DD-MM-YYYY`. |

#### Important Filename Note for Query-based URLs

When the generated variable is only in query parameters, the URL path can stay constant. In those cases, the default `save.filename_template: {{.ORIGINAL_FILENAME}}` may produce repeated filenames.

Example:

- Path-based variable (usually unique original names): `https://example.com/files/{{.N}}.zip`
- Query-based variable (path may stay the same): `https://example.com/files?file={{.N}}.zip`

For query-based patterns, prefer setting `save.filename_template` explicitly, for example using `{{.URL}}`.

#### Examples for All `url_gen_*` Variants

`url_gen_range` example:

```yaml
downloads:
  generated_range:
    url: https://example.com/files/{{.N}}.zip
    url_gen_range:
      var_name: N
      range_start: 1
      range_end: 10
```

`url_gen_list` example:

```yaml
downloads:
  generated_list:
    url: https://example.com/files/{{.NAME}}.zip
    url_gen_list:
      var_name: NAME
      values: [report-a, report-b, report-c]
```

`url_gen_file` example:

```yaml
downloads:
  generated_file:
    url: https://example.com/files/{{.ID}}.zip
    url_gen_file:
      var_name: ID
      file: ./ids.txt
```

Full `url_gen_dates` example with query parameter and deterministic filename:

```yaml
downloads:
  example_dynamic_urls_date_range_no_filename_in_url:
    url: https://example.com/files?date={{.DATE}}
    url_gen_dates:
      var_name: DATE
      date_range_start: "2026-01-01"
      date_range_end: "2026-01-02"
      date_format: YYYY-MM-DD
    save:
      filename_template: "report-{{.URL}}.pdf"
```

See also: [4) Reliability and Fail-fast](#4-reliability-and-fail-fast), [Validation Rules and Failure Modes](#validation-rules-and-failure-modes).

### 4) Reliability and Fail-fast

Use this when endpoints are unstable or CI must fail deterministically.

#### Retry Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `retry` | `bool` or `object` | `false` | No | `true` enables retry defaults. Object enables retry with explicit fields. |
| `retry.on_runtime_error` | `bool` | `true` (when retry is enabled) | No | Runtime/network errors retry when true. |
| `retry.on_http_code` | `[]int` | `[]` | No | If empty: retry all status codes `>= 400`. If set: retry only listed codes. |
| `retry.retries` | `int` | `3` | No | Number of retries after initial attempt. Must be `>= 0`. |
| `retry.delay` | `int` | `1` | No | Base delay in seconds. Must be `>= 0`. |
| `retry.exponential_backoff` | `bool` | `true` | No | Delay scales as `delay * 2^(attempt-1)`. |

#### Stop-All Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `stop_all_tasks.on_runtime_error` | `bool` | `false` | No | Runtime error marks task result as abort-all. |
| `stop_all_tasks.on_http_code` | `[]int` | `[]` | No | Matching status code marks task result as abort-all after retries end. |

#### Warning Promotion and Limits

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `treat_warnings_as_errors` | `bool` | `false` | No | Warning messages are promoted to errors at task finalization. |
| `download_limit` | `int` | `0` | No | Truncates candidate URL list before download loop. |
| `selector_matches_limit` | `int` | `0` | No | Limits extraction pass per page. |
| `timeout` | `int` | `0` | No | Request timeout seconds. |

Minimal example:

```yaml
downloads:
  resilient_download:
    url: https://example.com/file.zip
    retry: true
```

Advanced example:

```yaml
downloads:
  strict_ci:
    url: https://example.com/listing
    selector: a
    treat_warnings_as_errors: true
    retry:
      on_runtime_error: true
      on_http_code: [429, 500, 502, 503, 504]
      retries: 4
      delay: 2
      exponential_backoff: true
    stop_all_tasks:
      on_runtime_error: true
      on_http_code: [401, 403]
```

See also: [Interaction Matrix (Implicit Behavior)](#interaction-matrix-implicit-behavior).

### 5) Authentication and Security

Use this when tasks need credentials and trust restrictions.

#### Authentication Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `auth.type` | `string` | none | Conditional | Required when `auth` is set. Allowed: `basic`, `bearer`, `api_key`. |
| `auth.username` | `string` | `""` | Conditional | Required for `basic`. |
| `auth.password` | `string` | `""` | Conditional | Required for `basic`. |
| `auth.token` | `string` | `""` | Conditional | Required for `bearer`. |
| `auth.key_name` | `string` | `""` | Conditional | Required for `api_key`. |
| `auth.key_value` | `string` | `""` | Conditional | Required for `api_key`. |
| `auth.key_in` | `string` | `header` | No | Allowed: `header`, `query`, `cookie`. |

#### Security Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `security.follow_redirects` | `bool` | `true` | No | Redirect target is still validated by domain/scheme policy. |
| `security.allow_insecure_http` | `bool` | `false` | No | Must be true to allow `http://` URLs. |
| `security.allow_self_signed_cert` | `bool` | `false` | No | Enables TLS skip-verify mode. |
| `security.allowed_domains` | `[]string` | `[]` | No | If non-empty, only exact domain/subdomain matches are allowed. |
| `security.allowed_file_types` | `[]string` | `[]` | No | Extension/content-type allow-list. Values may include leading dot or not. |

Minimal example:

```yaml
downloads:
  private_file:
    url: https://secure.example.com/file.zip
    auth:
      type: bearer
      token: $API_TOKEN
```

Advanced example:

```yaml
downloads:
  strict_policy:
    url: https://assets.example.com/list
    selector: a
    auth:
      type: api_key
      key_name: X-API-Key
      key_value: $API_KEY
      key_in: header
    security:
      follow_redirects: true
      allow_insecure_http: false
      allow_self_signed_cert: false
      allowed_domains: [assets.example.com, cdn.example.com]
      allowed_file_types: [zip, pdf]
```

See also: [Validation Rules and Failure Modes](#validation-rules-and-failure-modes), [Interaction Matrix (Implicit Behavior)](#interaction-matrix-implicit-behavior).

### 6) File Save and Naming

Use this when output naming, collisions, and metadata need strict control.

#### Save Options

| Option | Type | Default | Req. | Notes |
|---|---|---|---|---|
| `save.base_download_dir` | `string` | `.` | No | Relative paths resolve from config file directory. |
| `save.filename_template` | `string` | `{{.ORIGINAL_FILENAME}}` | No | Used when `save.filename` is empty. |
| `save.filename` | `string` | `""` | No | Fixed name. Invalid when task can resolve multiple URLs. |
| `save.override_if_exists` | `bool` | `false` | No | Overwrites only when `force_download: true`. |
| `save.extension` | `string` | `""` | No | Extension override. |
| `save.save_metadata` | `bool` | `false` | No | Writes `<saved_base>_metadata.json`. |

#### Filename Template Variables

| Variable | Description | Example |
|---|---|---|
| `{{.ORIGINAL_FILENAME}}` | Name inferred from URL path | `report.pdf` |
| `{{.URL}}` | Sanitized full URL | `https_example.com_report.pdf` |
| `{{.DOMAIN}}` | URL hostname | `example.com` |
| `{{.TIMESTAMP}}` | UTC RFC3339 timestamp | `2026-03-31T01:02:23Z` |
| `{{.DATE}}` | UTC date | `2026-03-31` |
| `{{.TIME}}` | UTC time | `01-02-23` |
| `{{.UUID}}` | Random UUID | `bd80e0d3-ec89-4458-af5c-857f21cccb89` |

Minimal example:

```yaml
downloads:
  named_output:
    url: https://example.com/file.pdf
    save:
      filename_template: "{{.DATE}}_{{.ORIGINAL_FILENAME}}"
```

Advanced example:

```yaml
downloads:
  immutable_versions:
    url: https://example.com/file.pdf
    force_download: true
    save:
      base_download_dir: ./archive
      filename_template: "{{.DOMAIN}}_{{.TIMESTAMP}}_{{.ORIGINAL_FILENAME}}"
      extension: pdf
      override_if_exists: false
      save_metadata: true
```

See also: [Interaction Matrix (Implicit Behavior)](#interaction-matrix-implicit-behavior).

## URL Generation and Sub-object Notes

### `retry` as Boolean vs Object

| Input form | Meaning |
|---|---|
| `retry: false` | Retry disabled |
| `retry: true` | Retry enabled with defaults |
| `retry: { ... }` | Retry enabled with custom fields |

### `auth.type` Runtime Behavior

| Type | Runtime behavior |
|---|---|
| `basic` | Sets HTTP basic auth from `username` + `password` |
| `bearer` | Sets `Authorization: Bearer <token>` |
| `api_key` | Adds key/value to header, query string, or cookie based on `key_in` |

## Interaction Matrix (Implicit Behavior)

### Retry, Stop, and Warning Interactions

| Scenario | Effective behavior |
|---|---|
| `retry: true` without `retry.on_http_code` | Retries all HTTP status codes `>= 400` and runtime errors (`on_runtime_error` default true). |
| `retry.on_http_code` set | Retries only listed HTTP codes. Runtime errors still depend on `on_runtime_error`. |
| `stop_all_tasks.on_http_code` with retry enabled | Stop-on-code check is evaluated after retry loop is exhausted. |
| `treat_warnings_as_errors: true` + retry enabled + `on_runtime_error: true` | Warning-only failures are promoted and retried. |
| `treat_warnings_as_errors: true` + `stop_all_tasks.on_runtime_error: true` | Promoted warning errors can set abort-all. |

### Save/Overwrite/Naming Interactions

| Scenario | Effective behavior |
|---|---|
| Existing file + `force_download: false` | File is skipped with warning reason "file already exists". |
| Existing file + `force_download: true` + `save.override_if_exists: true` | Existing file is overwritten in place. |
| Existing file + `force_download: true` + `save.override_if_exists: false` | New path is generated with suffix `_1`, `_2`, ... |
| URL varies only by query params + default `save.filename_template` | Filename may repeat because default template uses path-derived `ORIGINAL_FILENAME`; use `save.filename_template` like `report-{{.URL}}.pdf` for uniqueness. |
| `save.filename` + `save.filename_template` | `save.filename` wins. Template is ignored and warning diagnostic is emitted. |
| `save.filename` with selector/url_gen tasks | Invalid config because task may resolve multiple URLs. |

### Selector and URL Candidate Limits

| Scenario | Effective behavior |
|---|---|
| `selector_matches_limit > 0` | Caps extraction loop per page. |
| `download_limit > 0` | Caps final candidate URL list before download phase. |
| Empty `selector` | No HTML extraction. Seed URLs are downloaded directly. |
| `max_depth > 1` with selector | Breadth-first traversal up to configured depth. |
| `max_depth > 1` without selector | No runtime effect; validation warning is emitted. |

### Security Interactions

| Scenario | Effective behavior |
|---|---|
| `allow_insecure_http: false` | `http://` URLs are blocked. |
| `allowed_domains` set | Exact domain/subdomain filter applies to direct URLs and redirects. |
| `allowed_file_types` set and URL has no extension | URL is blocked by extension policy. |
| `allowed_file_types` set | Both URL extension and response content-type are checked. |

## Validation Rules and Failure Modes

### Structural and Value Validation

| Rule | Result on violation |
|---|---|
| `url` missing | Error |
| `task_concurrency <= 0` or `downloads_per_task_concurrency <= 0` | Error |
| More than one `url_gen_*` block in same task | Error |
| `url_gen_range.step == 0` | Error |
| `url_gen_range.range_end < range_start` | Error |
| `url_gen_list.values` empty | Error |
| Invalid `url_gen_dates` format/date/order | Error |
| URL template missing declared `var_name` | Error |
| Invalid `regex_filter` or `attr_val_regex_filter` | Error |
| `timeout < 0`, `download_limit < 0`, `selector_matches_limit < 0` | Error |
| Retry/stop HTTP code outside `100..599` | Error |
| Invalid/missing auth fields for chosen `auth.type` | Error |
| Invalid `auth.key_in` value | Error |
| Empty item in `allowed_domains` or `allowed_file_types` | Error |
| `save.filename` with selector or any `url_gen_*` | Error |
| Top-level `base_download_dir` | Warning (deprecated) |
| `save.filename_template` set together with `save.filename` | Warning (template ignored) |
| `max_depth > 1` while selector is empty | Warning (no effect) |

### Runtime Warning Conditions

| Condition | Default outcome | With `treat_warnings_as_errors: true` |
|---|---|---|
| No generated seed URLs | Warning, task completes | Error |
| Selector resolves zero candidate URLs | Warning, task completes | Error |
| Existing file skipped (`force_download: false`) | Warning entry | Error |

## Metadata Output Schema

When `save.save_metadata: true`, a JSON file is written beside the downloaded file.

| Metadata key | Type | Description |
|---|---|---|
| `main_url` | `string` | Configured task URL (`downloads.<name>.url`). |
| `download_url` | `string` | Final URL used for request. |
| `original_filename` | `string` | Filename inferred from URL path. |
| `saved_filename` | `string` | Final filename after template/extension/collision resolution. |
| `timestamp` | `string` | UTC RFC3339 save timestamp. |
| `size` | `number` | Downloaded byte size. |
| `mime_type` | `string` | Response `Content-Type`. |
| `download_task_name` | `string` | Task key under `downloads`. |

## Global + Task Merge Semantics

| Category | Merge behavior |
|---|---|
| Scalars (`selector`, `timeout`, etc.) | Task value wins when explicitly set; otherwise inherited from `config`. |
| `retry` | Task `retry` merges with global retry defaults when both are enabled. |
| `auth` | Task `auth` can inherit unset fields from global `auth`. |
| `security` | Security fields inherit per-field from global defaults when task field is unset. |

## Option Coverage Checklist

All canonical option paths below are covered in this document.

| Field path | Covered in section |
|---|---|
| `env_file` | [Root Options](#root-options) |
| `env` | [Root Options](#root-options) |
| `config.task_concurrency` | [Worker Options](#worker-options) |
| `config.downloads_per_task_concurrency` | [Worker Options](#worker-options) |
| `config.log_level` | [Worker Options](#worker-options) |
| `config.default_attr_mappings` | [default_attr_mappings](#default_attr_mappings) |
| `config.selector` | [Global Default Task Options](#global-default-task-options) |
| `config.attr` | [Global Default Task Options](#global-default-task-options) |
| `config.regex_filter` | [Global Default Task Options](#global-default-task-options) |
| `config.attr_val_regex_filter` | [Global Default Task Options](#global-default-task-options) |
| `config.url_gen_range` | [Global Default Task Options](#global-default-task-options) |
| `config.url_gen_list` | [Global Default Task Options](#global-default-task-options) |
| `config.url_gen_file` | [Global Default Task Options](#global-default-task-options) |
| `config.url_gen_dates` | [Global Default Task Options](#global-default-task-options) |
| `config.force_download` | [Global Default Task Options](#global-default-task-options) |
| `config.treat_warnings_as_errors` | [Global Default Task Options](#global-default-task-options) |
| `config.stop_all_tasks.on_runtime_error` | [Global Default Task Options](#global-default-task-options) |
| `config.stop_all_tasks.on_http_code` | [Global Default Task Options](#global-default-task-options) |
| `config.timeout` | [Global Default Task Options](#global-default-task-options) |
| `config.download_limit` | [Global Default Task Options](#global-default-task-options) |
| `config.selector_matches_limit` | [Global Default Task Options](#global-default-task-options) |
| `config.retry` | [Global Default Task Options](#global-default-task-options) |
| `config.env` | [Global Default Task Options](#global-default-task-options) |
| `config.auth` | [Global Default Task Options](#global-default-task-options) |
| `config.security` | [Global Default Task Options](#global-default-task-options) |
| `config.max_depth` | [Global Default Task Options](#global-default-task-options) |
| `config.save.base_download_dir` | [Global Default Task Options](#global-default-task-options) |
| `config.save.filename_template` | [Global Default Task Options](#global-default-task-options) |
| `config.save.filename` | [Global Default Task Options](#global-default-task-options) |
| `config.save.override_if_exists` | [Global Default Task Options](#global-default-task-options) |
| `config.save.extension` | [Global Default Task Options](#global-default-task-options) |
| `config.save.save_metadata` | [Global Default Task Options](#global-default-task-options) |
| `config.base_download_dir` | [Global Default Task Options](#global-default-task-options) |
| `downloads.<name>.url` | [Core Task Options](#core-task-options) |
| `downloads.<name>.downloads_per_task_concurrency` | [Worker Options](#worker-options) and [Global + Task Merge Semantics](#global--task-merge-semantics) |
| `downloads.<name>.selector` | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |
| `downloads.<name>.attr` | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |
| `downloads.<name>.regex_filter` | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |
| `downloads.<name>.attr_val_regex_filter` | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |
| `downloads.<name>.url_gen_range.*` | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) |
| `downloads.<name>.url_gen_list.*` | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) |
| `downloads.<name>.url_gen_file.*` | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) |
| `downloads.<name>.url_gen_dates.*` | [3) Generate URLs Dynamically](#3-generate-urls-dynamically) |
| `downloads.<name>.force_download` | [1) Static Single URL Download](#1-static-single-url-download) |
| `downloads.<name>.treat_warnings_as_errors` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.save.*` | [6) File Save and Naming](#6-file-save-and-naming) |
| `downloads.<name>.base_download_dir` | [Global Default Task Options](#global-default-task-options) and [Validation Rules and Failure Modes](#validation-rules-and-failure-modes) |
| `downloads.<name>.stop_all_tasks.*` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.timeout` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.download_limit` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.selector_matches_limit` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.retry.*` | [4) Reliability and Fail-fast](#4-reliability-and-fail-fast) |
| `downloads.<name>.env` | [Core Task Options](#core-task-options) and [Environment Variable Precedence](#environment-variable-precedence) |
| `downloads.<name>.auth.*` | [5) Authentication and Security](#5-authentication-and-security) |
| `downloads.<name>.security.*` | [5) Authentication and Security](#5-authentication-and-security) |
| `downloads.<name>.max_depth` | [2) Discover Links from HTML (Selector)](#2-discover-links-from-html-selector) |

