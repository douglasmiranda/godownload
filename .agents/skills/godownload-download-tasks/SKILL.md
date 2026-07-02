---
name: godownload-download-tasks
description: 'Create and validate godownload/godownloader download tasks in YAML. Use when authoring new entries under downloads:, choosing selector vs URL generation, setting save/retry/security/auth options, and checking task behavior against project conventions in DOWNLOAD.md.'
argument-hint: 'Describe the file source, task goal, and required options (selector, URL generation, save, retry, security, auth).'
user-invocable: true
---

# Create Godownload Download Tasks

## What This Skill Produces
- A valid task entry under `downloads:` for this repository's configuration format.
- Optional companion recommendations for `config`, `env_file`, and `env` when needed.
- A short validation checklist to confirm behavior before running the worker.

Primary reference: DOWNLOAD.md in the root of the repository.

## When To Use
- You are creating a new download task for this repo.
- You need to choose between direct URL, selector extraction, or dynamic URL generation.
- You need to add save naming rules, retry behavior, auth, security policy, or stop-all behavior.
- You want the final task to match project conventions shown in DOWNLOAD.md.

## Procedure
1. Identify task intent and source shape.
- Direct file URL: use `url` only.
- HTML page with links: use `url` + `selector` (+ `attr` only if not covered by default mappings).
- Patterned or list-driven links: use `url_gen_range`, `url_gen_list`, `url_gen_file`, or `url_gen_dates`.

2. Choose link selection strategy.
- Use `selector` for one or many matches from HTML.
- Add `regex_filter` when filtering by element text.
- Add `attr_val_regex_filter` when filtering by extracted URL value.
- Add `selector_matches_limit` if page parsing should stop early.

3. Configure output behavior.
- Use `save.base_download_dir` for destination.
- Use `save.filename` only for single-file tasks.
- Use `save.filename_template` for deterministic naming across many files.
- If re-running tasks, decide `force_download` and `save.override_if_exists` together.
- Add `save.extension` or `save.save_metadata` only when required.

4. Configure reliability and failure policy.
- Use `timeout` and `download_limit` when needed.
- Use `retry: true` for defaults, or structured `retry` for explicit HTTP/runtime logic.
- Use `stop_all_tasks.on_runtime_error` or `stop_all_tasks.on_http_code` for fail-fast workflows.
- Use `treat_warnings_as_errors: true` for CI-sensitive tasks.
- Use `hooks` for simple shell follow-up commands after task success, failure, or completion.

5. Configure security/auth only when required.
- Security controls: `allow_insecure_http`, `allow_self_signed_cert`, `allowed_domains`, `allowed_file_types`.
- Auth controls: `basic`, `bearer`, or `api_key`.
- Prefer env variables for secrets (`env_file`, global `env`, or task-level `env`).

6. Validate task compatibility.
- Ensure exactly one URL source pattern is coherent for the task.
- Ensure `save.filename` is not used when multiple files can match.
- Ensure selectors and filters can actually match expected elements.
- Ensure retry/stop rules do not conflict with desired continuation behavior.
- Ensure hook failures intentionally warn or fail the task via `hooks.fail_task_if_error`.
- Ensure security policy allows the target URL and file type.

## Decision Hints
- Need many deterministic URLs: prefer `url_gen_range` or `url_gen_dates`.
- Need full URLs from external list: set `url: "{{.URL}}"` + `url_gen_file`.
- Need strict CI behavior: combine `treat_warnings_as_errors` with stop/retry settings.
- Need predictable deduping/overwrites: set naming (`filename_template`) before deciding override policy.

## Completion Checks
- Task YAML is syntactically valid and nested under `downloads:`.
- Field names and value types match examples in DOWNLOAD.md.
- Save/retry/security/auth options are intentionally selected, not copied blindly.
- Expected behavior for existing files, warnings, and runtime errors is explicit.
- If secrets are needed, they come from env sources instead of hardcoding.
