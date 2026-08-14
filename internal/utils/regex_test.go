package utils

import "testing"

func TestIsUsableName(t *testing.T) {
	usable := []string{
		"Movie",
		"Great.Movie.2024",
		".hidden",                      // dotfile: has a meaningful rune
		"www.UIndex.org    Some.Movie", // multi-space release name is legitimate
		"..b",                          // leading dots are fine when a real rune follows
		"a",
	}
	for _, name := range usable {
		if !IsUsableName(name) {
			t.Errorf("IsUsableName(%q) = false, want true", name)
		}
	}

	unusable := []string{
		"",    // empty
		".",   // filepath.Join(dir, ".") == dir
		"..",  // parent traversal
		"...", // collapses under Clean
		"   ", // whitespace only
		" . ", // dots + whitespace only
		". .", // dots + whitespace only
		"\t",  // tab only
	}
	for _, name := range unusable {
		if IsUsableName(name) {
			t.Errorf("IsUsableName(%q) = true, want false", name)
		}
	}
}
