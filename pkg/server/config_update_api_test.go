package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	json "github.com/bytedance/sonic"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// apiTestConfigJSON mirrors the production shape that was lost: two configured
// providers with api keys, plus an arr.
const apiTestConfigJSON = `{
  "log_level": "info",
  "download_folder": "/downloads",
  "debrids": [
    {"name": "rd", "provider": "realdebrid", "api_key": "rd-key"},
    {"name": "tb", "provider": "torbox", "api_key": "tb-key"}
  ],
  "arrs": [{"name": "radarr", "host": "http://radarr:7878", "token": "tok"}]
}`

// setupConfigAPITest seeds a config.json fixture and returns a Server wired to
// a real Manager (handleUpdateConfig calls s.manager.Arr()).
func setupConfigAPITest(t *testing.T) (*Server, string) {
	t.Helper()
	config.Reset()
	t.Cleanup(config.Reset)
	dir := t.TempDir()
	config.SetConfigPath(dir)
	cfgFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgFile, []byte(apiTestConfigJSON), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}
	if cfg := config.Get(); len(cfg.Debrids) != 2 {
		t.Fatalf("fixture did not load: %+v", cfg.Debrids)
	}
	m := manager.New()
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Errorf("Stop manager: %v", err)
		}
	})
	return &Server{manager: m}, cfgFile
}

func postConfigUpdate(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleUpdateConfig(rec, req)
	return rec
}

func readSavedConfig(t *testing.T, cfgFile string) config.Config {
	t.Helper()
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	return saved
}

// TestHandleUpdateConfigPartialPostPreservesSections reproduces the incident:
// a POST without the "debrids" key must not wipe the configured providers
// (api keys included) from disk.
func TestHandleUpdateConfigPartialPostPreservesSections(t *testing.T) {
	s, cfgFile := setupConfigAPITest(t)

	rec := postConfigUpdate(t, s, `{"log_level":"debug"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved := readSavedConfig(t, cfgFile)
	if saved.LogLevel != "debug" {
		t.Fatalf("posted log_level not applied: %q", saved.LogLevel)
	}
	if len(saved.Debrids) != 2 {
		t.Fatalf("partial POST wiped debrid providers: %+v", saved.Debrids)
	}
	if saved.Debrids[0].APIKey != "rd-key" || saved.Debrids[1].APIKey != "tb-key" {
		t.Fatalf("api keys lost: %+v", saved.Debrids)
	}
	if len(saved.Arrs) != 1 || saved.Arrs[0].Token != "tok" {
		t.Fatalf("partial POST wiped arrs: %+v", saved.Arrs)
	}
	if saved.DownloadFolder != "/downloads" {
		t.Fatalf("partial POST wiped download_folder: %q", saved.DownloadFolder)
	}
}

// TestHandleUpdateConfigExplicitEmptyStillClears: an explicitly posted empty
// section is a real instruction and must clear, exactly as before the merge
// fix.
func TestHandleUpdateConfigExplicitEmptyStillClears(t *testing.T) {
	s, cfgFile := setupConfigAPITest(t)

	rec := postConfigUpdate(t, s, `{"debrids":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved := readSavedConfig(t, cfgFile)
	if len(saved.Debrids) != 0 {
		t.Fatalf("explicit empty debrids did not clear: %+v", saved.Debrids)
	}
	if len(saved.Arrs) != 1 {
		t.Fatalf("omitted arrs must survive an explicit debrids clear: %+v", saved.Arrs)
	}
}

// TestHandleUpdateConfigFullPostOverwrites: a full-config POST (every key the
// web UI sends) keeps the pre-fix behavior — posted values win wholesale.
func TestHandleUpdateConfigFullPostOverwrites(t *testing.T) {
	s, cfgFile := setupConfigAPITest(t)

	body := `{
		"log_level": "trace",
		"url_base": "/",
		"bind_address": "127.0.0.1",
		"app_url": "",
		"port": "9999",
		"allowed_file_types": ["mkv"],
		"min_file_size": "",
		"max_file_size": "",
		"remove_stalled_after": "10m",
		"nzb_user_agent": "",
		"download_folder": "/new-downloads",
		"refresh_interval": "30s",
		"default_download_action": "symlink",
		"max_active_downloads": 7,
		"skip_pre_cache": false,
		"always_rm_tracker_urls": false,
		"folder_naming": "",
		"disable_webdav": false,
		"refresh_dirs": "",
		"custom_folders": {},
		"debrids": [{"name": "new", "provider": "realdebrid", "api_key": "new-key"}],
		"arrs": [],
		"queue_cleanup": {"rules": []},
		"mount": {"type": "none", "mount_path": "/mnt"},
		"usenet": {"providers": []},
		"notifications": {"enabled": false},
		"repair": {"enabled": false}
	}`
	rec := postConfigUpdate(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	saved := readSavedConfig(t, cfgFile)
	if len(saved.Debrids) != 1 || saved.Debrids[0].APIKey != "new-key" {
		t.Fatalf("full POST did not replace debrids wholesale: %+v", saved.Debrids)
	}
	if len(saved.Arrs) != 0 {
		t.Fatalf("full POST with empty arrs did not clear them: %+v", saved.Arrs)
	}
	if saved.DownloadFolder != "/new-downloads" || saved.LogLevel != "trace" {
		t.Fatalf("full POST fields not applied: folder=%q level=%q", saved.DownloadFolder, saved.LogLevel)
	}
}
