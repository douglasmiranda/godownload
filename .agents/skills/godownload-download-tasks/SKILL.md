---
name: godownload-download-tasks
description: 'Creates and validates Godownload download tasks in YAML. Use when adding or changing entries under downloads:, choosing static URLs, URL generation, or HTML discovery, and configuring save, retry, hooks, security, authentication, or environment behavior.'
argument-hint: 'Describe the source, expected files, destination, and any discovery, naming, reliability, security, or authentication requirements.'
user-invocable: true
---

# Create Godownload Download Tasks

## Authority and Scope

- Read the relevant section of repository-root `DOWNLOAD.md` before editing. The code is authoritative when documentation and implementation differ; the schema, defaults, and validation live in `internal/config`.
- Inspect the target YAML first. Reuse inherited `config` defaults and preserve unrelated root settings and tasks.
- Change only the requested task and any necessary shared `config`, `env_file`, or `env` values.
- Do not invent selectors, credentials, allow-listed domains, filenames, or failure policy. Ask when a missing value materially changes behavior or security.
- When editing e2e fixtures, follow `e2e-tests/success`, `warnings`, and `failures`: success tasks must have no warnings, warning tasks must remain successful, and failure tasks must all fail.

## Workflow

1. Establish the contract:
   - task name;
   - source URL/page/list and expected number of files;
   - destination and naming/collision behavior;
   - authentication/security constraints;
   - timeout, retry, warning, and fail-fast expectations.

2. Build the URL pipeline:
   - Every task requires `url`.
   - A static file uses `url` without `selector` or `url_gen_*`.
   - Configure at most one of `url_gen_range`, `url_gen_list`, `url_gen_file`, or `url_gen_dates`. Its `var_name` must appear in `url` as `{{.VAR_NAME}}`.
   - A generator creates seed URLs. A `selector` may then discover links from every seed page; generation and discovery are valid together.
   - For a file containing complete URLs, use `url: "{{.URL}}"` with `url_gen_file.var_name: URL`. For paths or IDs, include the host/pattern around the variable.
   - Generated URLs are materialized in memory before `download_limit` is applied. Bound large ranges, date spans, lists, and files.

3. Configure HTML discovery only when `url` resolves to HTML pages:
   - `selector` is a CSS selector.
   - `attr` may be omitted for mapped tags (`a[href]`, `img[src]`, `link[href]`, `script[src]`, `iframe[src]`); set it for custom elements/attributes or add an intentional `config.default_attr_mappings` entry.
   - `regex_filter` matches trimmed element text; `attr_val_regex_filter` matches the raw extracted attribute value.
   - `selector_matches_limit` caps extracted URLs per page. `download_limit` caps the final candidate list.
   - Set `max_depth > 1` only for recursive selector crawling. Pages followed for links are not saved; only links on the last level, or links that are not HTML pages, are downloaded. A crawl reads at most 1000 linked pages (with a warning) and stops early once `download_limit` files are found, so set `download_limit` on broad selectors.
   - Discovered links that fail `allowed_domains`, `allow_insecure_http`, or `allowed_file_types` are skipped, not errors. A failing task URL page is an error.

4. Make naming safe:
   - Use `save.base_download_dir`; relative paths resolve from the YAML file's directory.
   - `save.filename` is valid only when neither `selector` nor any `url_gen_*` is configured.
   - For multiple URLs, use `save.filename_template`. The default `{{.ORIGINAL_FILENAME}}` can collide when URLs differ only by query parameters; include `{{.URL}}`, or another unique component, in that case.
   - Existing file behavior:
     - `force_download: false`: skip with a warning.
     - `force_download: true` + `save.override_if_exists: true`: overwrite.
     - `force_download: true` + `save.override_if_exists: false`: create `_1`, `_2`, and later suffixes.
   - Add `save.extension` and `save.save_metadata` only when required.

5. Add operational policy deliberately:
   - Shared task options may be placed under `config`; explicit task fields override them. Retry, auth, security, and hooks merge by field, and task `hooks.on_*: null` disables an inherited hook.
   - `retry: true` means three retries with one-second exponential backoff, runtime retries enabled, and HTTP `408`, `425`, `429`, `500`, `502`, `503`, `504` retryable. `Retry-After` is honored, and waits are capped at 5 minutes.
   - Use structured `retry` when retryable errors/codes, attempts, delay, or backoff must be explicit. Unset fields in the mapping keep the defaults; explicit `0` is respected.
   - `timeout` is an idle timeout (seconds without receiving data, default 30), not a total download limit.
   - `treat_warnings_as_errors` promotes zero matches, no generated URLs, existing-file skips, and non-fatal hook failures.
   - `stop_all_tasks` can cancel other tasks; add it only when requested.
   - Set `downloads_per_task_concurrency`, `timeout`, and limits only from an explicit requirement or an established local convention.
   - Hooks execute shell commands after task outcome. Do not add hooks merely to validate a task; if requested, choose `fail_task_if_error` intentionally and remember `on_complete` runs after success or failure.
   - Hooks run with `/bin/sh -c` (`cmd.exe /C` on Windows); the login shell is not used. Set `SHELL` in `config.env` or the task `env` only when commands need another shell, such as bash. Hooks receive `GODOWNLOAD_TASK`, `GODOWNLOAD_STATUS`, and `GODOWNLOAD_RESULT_FILE` (JSON with saved paths, URLs, sizes, and SHA-256).

6. Apply security and secrets:
   - HTTPS is required by default. Set `security.allow_insecure_http: true` only for an intentional HTTP source.
   - Redirect following defaults to enabled. `security.allowed_domains` must include source, discovered-link, and redirect destinations; it matches subdomains too. `security.allowed_file_types` requires the URL file name to end with the extension and checks the response content type (`application/octet-stream` and a missing type are accepted).
   - Consider `security.max_download_size`. Downloads stream to a temporary file, so file size is not limited by memory.
   - Auth credentials are sent only to the task URL host, a `url_gen_file` URL host, and `allowed_domains` hosts. When discovered links live on a CDN that needs the credentials, add that host to `allowed_domains`.
   - `security.allow_self_signed_cert` disables TLS certificate verification for the task URL hosts, a `url_gen_file` URL host, and `allowed_domains`; use only when explicitly required.
   - Auth uses `auth.type: basic`, `bearer`, or `api_key`. Put secret values in environment variables, not YAML literals.
   - Environment precedence is task `env`, `config.env`, root `env`, `env_file`, then process environment.

7. Validate without downloading:

```bash
go run ./.agents/skills/godownload-download-tasks/scripts/validate-config.go path/to/download.yaml
```

Fix every error and review every warning. This validator checks unknown YAML fields, environment expansion, defaults, and config rules; it cannot prove that remote selectors, credentials, content types, or URLs work. `go run ./cmd/godownload -config path/to/download.yaml -validate` performs the same check.

To check selectors and output paths against the real site without downloading files or running hooks, use a dry run. It fetches selector pages and `url_gen_file` URLs:

```bash
go run ./cmd/godownload -config path/to/download.yaml -tasks task_name -dry-run
```

Run the actual task only when network, file writes, and configured hooks are appropriate:

```bash
go run ./cmd/godownload -config path/to/download.yaml -tasks task_name
```

## Completion Standard

- Return the edited task or file and summarize assumptions.
- State the validation command and result.
- Call out expected file count/names, collision behavior, warning behavior, and any security relaxation.
- Never claim runtime success unless the task itself was run and its outputs were checked.
