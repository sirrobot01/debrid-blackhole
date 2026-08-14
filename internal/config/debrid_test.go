package config

import (
	"os"
	"strings"
	"testing"

	json "github.com/bytedance/sonic"
)

func boolPtr(value bool) *bool { return &value }

// TestDebridDownloadUncachedSaveRoundTrip reproduces the bug where
// POST /api/config with debrids[].download_uncached=false returned 200 but the
// key was stripped from disk (bool + omitempty), and any later save re-stripped
// a hand-edited false. With *bool, explicit values survive save round-trips
// while an absent key stays absent and resolves to the historical default
// (false).
func TestDebridDownloadUncachedSaveRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		fragment   string // JSON inside the debrid object, "" = key absent
		wantOnDisk bool
		want       *bool
	}{
		{name: "explicit false persists", fragment: `"download_uncached":false,`, wantOnDisk: true, want: boolPtr(false)},
		{name: "explicit true persists", fragment: `"download_uncached":true,`, wantOnDisk: true, want: boolPtr(true)},
		{name: "absent stays absent", fragment: ``, wantOnDisk: false, want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Reset()
			SetConfigPath(t.TempDir())
			t.Cleanup(Reset)

			// Simulate the API handler's decode of a POST body.
			body := `{"debrids":[{"name":"rd","provider":"realdebrid","api_key":"secret",` +
				test.fragment + `"rate_limit":"250/minute"}]}`
			var cfg Config
			if err := json.Unmarshal([]byte(body), &cfg); err != nil {
				t.Fatalf("unmarshal POST body: %v", err)
			}
			if err := cfg.Save(); err != nil {
				t.Fatalf("save config: %v", err)
			}

			verify := func(step string) {
				data, err := os.ReadFile(cfg.JsonFile())
				if err != nil {
					t.Fatalf("%s: read config file: %v", step, err)
				}
				if got := strings.Contains(string(data), `"download_uncached"`); got != test.wantOnDisk {
					t.Fatalf("%s: download_uncached key on disk = %v, want %v: %s", step, got, test.wantOnDisk, data)
				}
			}
			verify("first save")

			// Reload the persisted file the way loadConfig does (decode +
			// defaults) and confirm the tri-state value and its resolution.
			data, err := os.ReadFile(cfg.JsonFile())
			if err != nil {
				t.Fatalf("read config file: %v", err)
			}
			var loaded Config
			if err := json.Unmarshal(data, &loaded); err != nil {
				t.Fatalf("unmarshal persisted config: %v", err)
			}
			loaded.setDefaults()
			if len(loaded.Debrids) != 1 {
				t.Fatalf("expected 1 debrid after reload, got %d", len(loaded.Debrids))
			}
			got := loaded.Debrids[0].DownloadUncached
			switch {
			case test.want == nil && got != nil:
				t.Fatalf("expected nil DownloadUncached after reload, got %v", *got)
			case test.want != nil && (got == nil || *got != *test.want):
				t.Fatalf("DownloadUncached after reload = %v, want %v", got, *test.want)
			}
			wantEffective := test.want != nil && *test.want
			if loaded.Debrids[0].DownloadsUncached() != wantEffective {
				t.Fatalf("DownloadsUncached() = %v, want %v", loaded.Debrids[0].DownloadsUncached(), wantEffective)
			}

			// A later save (the re-strip path) must not change the on-disk
			// representation of the key.
			if err := loaded.Save(); err != nil {
				t.Fatalf("re-save config: %v", err)
			}
			verify("re-save")
		})
	}
}
