So, let's plan this before we progress to implementation, feel free to be critical about the decisions.

Also make sure to scan my main fine ./download.yml and cross check with the findings to make sure there's not much inconsistency, redundant configs, wrong assumptions, etc. This is important to find before we proceed to implementation, this is the time to be very analytical.

# GoDownload Prompt

This is a command-line tool for downloading files from the internet, with support for various configurations and features. The main configuration file is the download.yaml file, which contains the list of downloads and their configurations.

In download.yaml you will also find extensive examples and documentation in the comments.

## Implementation/Design Notes

IMPORTANT: ./download.yaml must be always be the point of reference for the implementation and design of the GoDownload App, as it contains the main configuration and examples for the app.

- Use Golang for the implementation, as it provides good support for concurrency and HTTP requests, which are essential for a download manager.
- Use a YAML parser library to read the download.yaml configuration file and parse the download configurations.
- Use up-to-date libraries/packages when necessary, but avoid unnecessary dependencies to keep the project lightweight and maintainable.
- Download files concurrently.
- Maybe separate the code into modules like:
  - config: for parsing the download.yaml file and validating the configurations.
  - godownloader: for handling the download logic, including making HTTP requests, handling errors, and saving files.
  - utils: for any utility functions that may be needed across the app (e.g. logging, filename generation, etc.).

### Suggestions

Not to be considered written in stone.

https://github.com/spf13/cobra looks good a good option for the command-line interface.

https://github.com/gocolly/colly looks good for web crawling and scraping. I don't know how flexibe is the selector and attribute configuration. If it fits this project's needs, awesome.

https://github.com/avelino/awesome-go is a great resource for finding up-to-date libraries. It's very extensive. 

### Specification and constraints for the implementation of the GoDownload App

Minimum required fields:
- url

Constraints for code:
- use depth limit to avoid infinite crawling
- use a timeout for the HTTP requests
- handle errors gracefully (e.g. log the error and continue with the next download)
- handle inputs carefully
  - url validation
  - type checking for the fields
  - date parsing for the date range in the dynamic URLs
  - regex validation for the regex filters
- handle downloaded files carefully
- fast fail when possible
- do not ignore errors, handle them gracefully and provide informative messages to the user
- Testing
 - make sure to implement parts like the parsers in a way to facilitate testing.
 - pure funcions that can be easily tested with different inputs and outputs are preferred when possible.
 - handle auth securely
