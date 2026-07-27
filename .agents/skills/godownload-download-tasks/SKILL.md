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
   - Set `max_depth > 1` only for recursive selector crawling.

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
   - `retry: true` means three retries with one-second exponential backoff, runtime retries enabled, and all HTTP statuses `>= 400` retryable.
   - Use structured `retry` when retryable errors/codes, attempts, delay, or backoff must be explicit.
   - `treat_warnings_as_errors` promotes zero matches, no generated URLs, existing-file skips, and non-fatal hook failures.
   - `stop_all_tasks` can cancel other tasks; add it only when requested.
   - Set `downloads_per_task_concurrency`, `timeout`, and limits only from an explicit requirement or an established local convention.
   - Hooks execute shell commands after task outcome. Do not add hooks merely to validate a task; if requested, choose `fail_task_if_error` intentionally and remember `on_complete` runs after success or failure.

6. Apply security and secrets:
   - HTTPS is required by default. Set `security.allow_insecure_http: true` only for an intentional HTTP source.
   - Redirect following defaults to enabled. `security.allowed_domains` must include source, discovered-link, and redirect destinations. `security.allowed_file_types` checks both URL extension and response content type.
   - Consider `security.max_download_size`; downloads are buffered in memory and `security.max_in_memory_size` defaults to `500mb`.
   - `security.allow_self_signed_cert` disables normal TLS certificate verification; use only when explicitly required.
   - Auth uses `auth.type: basic`, `bearer`, or `api_key`. Put secret values in environment variables, not YAML literals.
   - Environment precedence is task `env`, `config.env`, root `env`, `env_file`, then process environment.

7. Validate without downloading:

```bash
go run ./.agents/skills/godownload-download-tasks/scripts/validate-config.go path/to/download.yaml
```

Fix every error and review every warning. This validator checks strict YAML fields, environment expansion, defaults, and config rules; it cannot prove that remote selectors, credentials, content types, or URLs work.

Run the actual task only when network, file writes, and configured hooks are appropriate:

```bash
go run ./cmd/godownload -config path/to/download.yaml -tasks task_name
```

## Completion Standard

- Return the edited task or file and summarize assumptions.
- State the validation command and result.
- Call out expected file count/names, collision behavior, warning behavior, and any security relaxation.
- Never claim runtime success unless the task itself was run and its outputs were checked.
