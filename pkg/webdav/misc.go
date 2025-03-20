package webdav

import (
	"net/http"
	"net/url"
	"strings"
)

// getName: Returns the torrent name and filename from the path
// /webdav/alldebrid/__all__/TorrentName
func getName(rootDir, path string) (string, string) {
	path = strings.TrimPrefix(path, rootDir)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], strings.Join(parts[1:], "/")
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func isValidURL(str string) bool {
	u, err := url.Parse(str)
	// A valid URL should parse without error, and have a non-empty scheme and host.
	return err == nil && u.Scheme != "" && u.Host != ""
}
