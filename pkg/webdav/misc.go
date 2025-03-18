package webdav

import "strings"

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
