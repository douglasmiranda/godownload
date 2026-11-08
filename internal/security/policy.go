package security

import (
	"mime"
	"net/url"
	"path"
	"strings"
)

func IsURLAllowed(rawURL string, allowInsecureHTTP bool, allowedDomains []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "http" && !allowInsecureHTTP {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if len(allowedDomains) == 0 {
		return true
	}
	return HostMatchesAny(u.Hostname(), allowedDomains)
}

// HostMatchesAny reports whether host equals one of domains or is a subdomain
// of one. Entries may be written as "example.com", ".example.com" or
// "*.example.com".
func HostMatchesAny(host string, domains []string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	for _, d := range domains {
		norm := strings.ToLower(strings.TrimSpace(d))
		norm = strings.TrimPrefix(norm, "*.")
		norm = strings.TrimPrefix(norm, ".")
		norm = strings.TrimSuffix(norm, ".")
		if norm == "" {
			continue
		}
		if host == norm || strings.HasSuffix(host, "."+norm) {
			return true
		}
	}
	return false
}

// IsAllowedByExtension reports whether the URL path ends with one of the
// allowed file types. Multi-part types such as "tar.gz" are supported.
func IsAllowedByExtension(rawURL string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	name := strings.ToLower(path.Base(u.Path))
	if name == "" || name == "." || name == "/" {
		return false
	}
	for _, a := range allowed {
		norm := normalizeFileType(a)
		if norm != "" && strings.HasSuffix(name, "."+norm) {
			return true
		}
	}
	return false
}

// IsAllowedByContentType reports whether a response Content-Type is
// plausible for one of the allowed file types. Missing and generic binary
// content types are accepted because many servers send them for every file;
// the URL extension check still applies to those responses.
func IsAllowedByContentType(contentType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.ToLower(strings.Split(contentType, ";")[0]))
	}
	if mediaType == "" || genericBinaryTypes[mediaType] {
		return true
	}
	for _, a := range allowed {
		norm := normalizeFileType(a)
		if norm == "" {
			continue
		}
		for _, candidate := range mediaTypesFor(norm) {
			if mediaType == candidate {
				return true
			}
		}
	}
	return false
}

func normalizeFileType(v string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), ".")
}

var genericBinaryTypes = map[string]bool{
	"application/octet-stream":   true,
	"binary/octet-stream":        true,
	"application/download":       true,
	"application/x-download":     true,
	"application/force-download": true,
}

var knownMediaTypes = map[string][]string{
	"7z":   {"application/x-7z-compressed"},
	"bz2":  {"application/x-bzip2"},
	"csv":  {"text/csv", "application/csv", "text/plain"},
	"doc":  {"application/msword"},
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	"epub": {"application/epub+zip"},
	"gz":   {"application/gzip", "application/x-gzip"},
	"htm":  {"text/html"},
	"html": {"text/html"},
	"ico":  {"image/x-icon", "image/vnd.microsoft.icon"},
	"iso":  {"application/x-iso9660-image"},
	"jpeg": {"image/jpeg"},
	"jpg":  {"image/jpeg"},
	"js":   {"text/javascript", "application/javascript"},
	"json": {"application/json"},
	"md":   {"text/markdown", "text/plain"},
	"mp3":  {"audio/mpeg"},
	"mp4":  {"video/mp4"},
	"ppt":  {"application/vnd.ms-powerpoint"},
	"pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	"rar":  {"application/vnd.rar", "application/x-rar-compressed"},
	"svg":  {"image/svg+xml"},
	"tar":  {"application/x-tar"},
	"tgz":  {"application/gzip", "application/x-gzip"},
	"tif":  {"image/tiff"},
	"tiff": {"image/tiff"},
	"txt":  {"text/plain"},
	"xls":  {"application/vnd.ms-excel"},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	"xml":  {"application/xml", "text/xml"},
	"xz":   {"application/x-xz"},
	"zip":  {"application/zip", "application/x-zip-compressed"},
}

// mediaTypesFor returns the content types accepted for a file type such as
// "pdf" or "tar.gz".
func mediaTypesFor(fileType string) []string {
	ext := fileType
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i+1:]
	}
	out := []string{"application/" + ext, "image/" + ext, "text/" + ext, "audio/" + ext, "video/" + ext}
	return append(out, KnownMediaTypes(ext)...)
}

// KnownMediaTypes returns the media types files with extension ext (with or
// without the leading dot) are commonly served as, from a built-in table and
// the system MIME database. It returns nothing for unknown extensions.
func KnownMediaTypes(ext string) []string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	out := append([]string{}, knownMediaTypes[ext]...)
	if mt := mime.TypeByExtension("." + ext); mt != "" {
		if parsed, _, err := mime.ParseMediaType(mt); err == nil {
			out = append(out, parsed)
		}
	}
	return out
}
