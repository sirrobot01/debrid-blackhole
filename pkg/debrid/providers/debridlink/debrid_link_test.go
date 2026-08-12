package debridlink

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/request"
)

// doPostMultipart is SubmitMagnet's .torrent-upload path. On failure it used to
// drop the response body entirely (unlike the magnet-link path, which includes
// it), making a real API rejection much harder to diagnose than it needed to be.
func TestDoPostMultipart(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	t.Run("success decodes the body into result", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true, "value": {"id": "abc123", "name": "Test"}}`))
		}))
		defer srv.Close()

		dl := &DebridLink{Host: srv.URL, client: request.New()}
		var res SubmitTorrentInfo
		resp, body, err := dl.doPostMultipart("/seedbox/add", []byte("fake torrent bytes"), "file.torrent", &res)
		if err != nil {
			t.Fatalf("doPostMultipart: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
		}
		if len(body) == 0 {
			t.Error("body should not be empty on success")
		}
		if !res.Success || res.Value == nil || res.Value.ID != "abc123" {
			t.Errorf("result not decoded correctly: %+v", res)
		}
	})

	t.Run("failure returns the response body for the caller to surface", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "invalid torrent file"}`))
		}))
		defer srv.Close()

		dl := &DebridLink{Host: srv.URL, client: request.New()}
		var res SubmitTorrentInfo
		resp, body, err := dl.doPostMultipart("/seedbox/add", []byte("fake torrent bytes"), "file.torrent", &res)
		if err != nil {
			t.Fatalf("doPostMultipart: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("StatusCode = %d, want 400", resp.StatusCode)
		}
		if !strings.Contains(string(body), "invalid torrent file") {
			t.Errorf("body = %q, want it to contain the API's error detail", body)
		}
	})

	t.Run("empty body on success does not error, leaves result unset", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		dl := &DebridLink{Host: srv.URL, client: request.New()}
		var res SubmitTorrentInfo
		_, body, err := dl.doPostMultipart("/seedbox/add", []byte("fake torrent bytes"), "file.torrent", &res)
		if err != nil {
			t.Fatalf("doPostMultipart: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("body = %q, want empty", body)
		}
		if res.Success {
			t.Error("result should be left unset when the body is empty")
		}
	})
}
