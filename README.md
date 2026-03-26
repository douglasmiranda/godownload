# godownload - A simple command-line tool for downloading files from the internet.

## Features
- Supports downloading files from HTTP and HTTPS URLs.
- Allows users to specify a URL, search for links on a webpage and download files from those links.
- Allows users to specify the output directory and filename.
- Provides progress indication during the download process.
- Handles errors gracefully and provides informative messages.
- Specify downloads in a yaml file and download all files in the list.

## Design

The GoDownload App is designed to be flexible and extensible, allowing users to easily add new features and configurations as needed. The main configuration file is the download.yaml file, which contains the list of downloads and their configurations.

- download.yaml is the default configuration file


## Usage

Extensive documentation and examples can be found in the ./download.yaml file.

### Example download all PDFs from a webpage

download.yaml:

```yaml
downloads:
  # download the file from a specific link, based on the selector and attribute
  example_download_all_pdfs:
    # urls can be a direct link to the file or a page that contains the link to the file
    url: https://example.com/
    # <a id="download-link" href="https://example.com/file.pdf">Download</a>
    # <a id="download-link" href="https://example.com/file2.pdf">Download</a>
    selector: "a[href$='.pdf']"
```
