# AGENTS.md

## Packages/Files Roles

- `DOWNLOAD.md`: The main file. It contains examples of all configurations, descriptions of fields, and usage guidelines.
- `cmd`: Main entry point for godownload. It initializes the worker and starts the download process.
- `internal`: Core logic of godownload, including task processing, downloading, retry logic, and more. It is organized into subpackages for better modularity and maintainability.
- `internal/config`: Configuration structures and parsing logic for godownload. It defines how it reads and validates the configuration file.
- `internal/discovery`: Discovering and extracting URLs from HTML pages based on selectors and filters.
- `internal/downloader`: Logic for downloading files, it ends up making use of most of all the other modules under `internal` to perform the download tasks.
- `internal/env`: Handling environment variables and dotenv files for the download tasks.
- `internal/logging`: Logging, including the redaction of sensitive information in logs.
- `internal/security`: Enforcing security policies for download tasks.
- `internal/storage`: Saving downloaded files according to the specified naming and directory rules.
- `internal/urlgen`: Generating URLs based on patterns, ranges, lists, or date formats.
...

E2E tests. This doesn't substitute unit tests.

- `e2e-tests`: End-to-end tests in the form of YAML files.
- `e2e-tests/success`: Successful tasks. With no warnings.
- `e2e-tests/warnings`: Successful tasks. With warnings.
- `e2e-tests/failures`: All the tasks should fail.  