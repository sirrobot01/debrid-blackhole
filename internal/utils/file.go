package utils

import "strings"

func EscapePath(path string) string {
	// escape %
	escapedPath := strings.ReplaceAll(path, "%", "%25")

	// add others

	return escapedPath
}
