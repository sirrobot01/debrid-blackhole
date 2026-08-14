package parser

import "testing"

// determineNZBName must never return an unusable (collapsing) name when a
// usable source is available, and must return "" only when every source
// collapses — so Parse can substitute the unique NZB ID.
func TestDetermineNZBName(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		meta     map[string]string
		want     string
	}{
		{"valid filename strips ext", "Great.Movie.2024.nzb", nil, "Great.Movie.2024"},
		{"empty filename, no meta", "", nil, ""},
		{"bare extension collapses", ".nzb", nil, ""},
		{"all invalid chars collapse", "???.nzb", nil, ""},
		{"stars collapse", "***.nzb", nil, ""},
		{"collapsing filename falls back to meta Name", "???.nzb", map[string]string{"Name": "Fallback Movie"}, "Fallback Movie"},
		{"collapsing filename falls back to title", "***.nzb", map[string]string{"title": "Title Movie"}, "Title Movie"},
		{"meta Name preferred over title", "", map[string]string{"Name": "By Name", "title": "By Title"}, "By Name"},
		{"multi-space release name preserved", "www.UIndex.org    Some.Movie.mkv", nil, "www.UIndex.org    Some.Movie"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := determineNZBName(tc.filename, tc.meta); got != tc.want {
				t.Fatalf("determineNZBName(%q, %v) = %q, want %q", tc.filename, tc.meta, got, tc.want)
			}
		})
	}
}
