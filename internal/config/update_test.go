package config

import (
	"reflect"
	"testing"

	json "github.com/bytedance/sonic"
)

func preserveBaseConfig() *Config {
	return &Config{
		LogLevel:       "info",
		Port:           "8282",
		DownloadFolder: "/downloads",
		Debrids: []Debrid{
			{Name: "rd", Provider: "realdebrid", APIKey: "rd-key"},
			{Name: "tb", Provider: "torbox", APIKey: "tb-key"},
		},
		Arrs:   []Arr{{Name: "radarr", Host: "http://radarr:7878", Token: "tok"}},
		Usenet: Usenet{Providers: []UsenetProvider{{Host: "news.example", Username: "u", Password: "p"}}},
	}
}

// TestPreserveMissingSectionsKeepsOmittedSections covers the data loss: a POST
// without the "debrids" key must not wipe configured providers (api keys
// included) or any other omitted section.
func TestPreserveMissingSectionsKeepsOmittedSections(t *testing.T) {
	current := preserveBaseConfig()
	body := []byte(`{"log_level":"debug"}`)

	var updated Config
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if err := updated.PreserveMissingSections(current, body); err != nil {
		t.Fatalf("PreserveMissingSections: %v", err)
	}

	if updated.LogLevel != "debug" {
		t.Fatalf("posted log_level lost: %q", updated.LogLevel)
	}
	if len(updated.Debrids) != 2 || updated.Debrids[0].APIKey != "rd-key" || updated.Debrids[1].APIKey != "tb-key" {
		t.Fatalf("omitted debrids section was not preserved: %+v", updated.Debrids)
	}
	if len(updated.Arrs) != 1 || updated.Arrs[0].Token != "tok" {
		t.Fatalf("omitted arrs section was not preserved: %+v", updated.Arrs)
	}
	if len(updated.Usenet.Providers) != 1 {
		t.Fatalf("omitted usenet section was not preserved: %+v", updated.Usenet)
	}
	if updated.DownloadFolder != "/downloads" || updated.Port != "8282" {
		t.Fatalf("omitted scalar fields were not preserved: folder=%q port=%q", updated.DownloadFolder, updated.Port)
	}
}

// TestPreserveMissingSectionsExplicitEmptyStillClears: presence of the key,
// even with an empty value, means the caller wants that value.
func TestPreserveMissingSectionsExplicitEmptyStillClears(t *testing.T) {
	current := preserveBaseConfig()
	body := []byte(`{"debrids":[],"download_folder":""}`)

	var updated Config
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if err := updated.PreserveMissingSections(current, body); err != nil {
		t.Fatalf("PreserveMissingSections: %v", err)
	}

	if len(updated.Debrids) != 0 {
		t.Fatalf("explicitly posted empty debrids must clear, got %+v", updated.Debrids)
	}
	if updated.DownloadFolder != "" {
		t.Fatalf("explicitly posted empty download_folder must clear, got %q", updated.DownloadFolder)
	}
	if len(updated.Arrs) != 1 {
		t.Fatalf("omitted arrs must be preserved, got %+v", updated.Arrs)
	}
}

// TestPreserveMissingSectionsFullPostUnchanged: a body that posts every
// top-level key (the web UI's full-config save) must decode identically with
// and without the merge step.
func TestPreserveMissingSectionsFullPostUnchanged(t *testing.T) {
	current := preserveBaseConfig()
	body := []byte(`{
		"log_level": "trace",
		"url_base": "/",
		"bind_address": "127.0.0.1",
		"app_url": "http://app.example",
		"port": "9999",
		"allowed_file_types": ["mkv"],
		"min_file_size": "1MB",
		"max_file_size": "10GB",
		"remove_stalled_after": "10m",
		"nzb_user_agent": "agent",
		"download_folder": "/new-downloads",
		"refresh_interval": "30s",
		"default_download_action": "symlink",
		"max_active_downloads": 7,
		"skip_pre_cache": true,
		"always_rm_tracker_urls": true,
		"folder_naming": "original",
		"disable_webdav": true,
		"refresh_dirs": "/dirs",
		"custom_folders": {},
		"debrids": [{"name": "new", "provider": "realdebrid", "api_key": "new-key"}],
		"arrs": [],
		"queue_cleanup": {"rules": []},
		"mount": {"type": "none", "mount_path": "/mnt"},
		"usenet": {"providers": []},
		"notifications": {"enabled": false},
		"repair": {"enabled": false}
	}`)

	var plain, merged Config
	if err := json.Unmarshal(body, &plain); err != nil {
		t.Fatalf("unmarshal body (plain): %v", err)
	}
	if err := json.Unmarshal(body, &merged); err != nil {
		t.Fatalf("unmarshal body (merged): %v", err)
	}
	if err := merged.PreserveMissingSections(current, body); err != nil {
		t.Fatalf("PreserveMissingSections: %v", err)
	}
	if !reflect.DeepEqual(plain, merged) {
		t.Fatalf("full-config POST changed by merge:\nplain:  %+v\nmerged: %+v", plain, merged)
	}
	if len(merged.Debrids) != 1 || merged.Debrids[0].APIKey != "new-key" {
		t.Fatalf("posted debrids did not win wholesale: %+v", merged.Debrids)
	}
	if len(merged.Arrs) != 0 {
		t.Fatalf("posted empty arrs did not clear: %+v", merged.Arrs)
	}
}
