package sabnzbd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// newSABAuthHarness wires a bare Manager into the SABnzbd shim. The auth path
// never touches usenet — it only reads and mutates the shared arr map.
func newSABAuthHarness(t *testing.T) (*SABnzbd, *manager.Manager) {
	t.Helper()
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().UseAuth = false
	m := manager.New()
	t.Cleanup(func() {
		if err := m.Stop(); err != nil {
			t.Errorf("Stop manager: %v", err)
		}
	})
	return New(m), m
}

// sabHealthOKServer answers 200 to the arr health probe so Arr.Validate()
// completes quickly and deterministically during authenticate.
func sabHealthOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthenticateDoesNotWipeAutoArrOnEmptyCredentials is the SABnzbd-side
// regression guard for the shared-arr corruption bug. A SAB poll with empty
// ma_username/ma_password must NOT wipe an already-populated auto arr's
// Host/Token, which would corrupt the shared arr map for every other consumer.
func TestAuthenticateDoesNotWipeAutoArrOnEmptyCredentials(t *testing.T) {
	srv := sabHealthOKServer(t)
	s, m := newSABAuthHarness(t)

	const category = "sonarr"
	seeded := arr.New(category, srv.URL, "seed-token", false, nil, "", string(arr.SourceAuto))
	m.Arr().AddOrUpdate(seeded)

	got, err := s.authenticate(category, "", "")
	if err != nil {
		t.Fatalf("authenticate with empty creds returned error: %v", err)
	}
	if got.Host != srv.URL {
		t.Fatalf("Host wiped by empty-cred poll: got %q, want %q", got.Host, srv.URL)
	}
	if got.Token != "seed-token" {
		t.Fatalf("Token wiped by empty-cred poll: got %q, want %q", got.Token, "seed-token")
	}

	shared := m.Arr().Get(category)
	if shared == nil {
		t.Fatalf("shared arr disappeared from the map")
	}
	if shared.Host != srv.URL || shared.Token != "seed-token" {
		t.Fatalf("shared arr corrupted: Host=%q Token=%q", shared.Host, shared.Token)
	}
}

// TestAuthenticatePopulatesAutoArrWithValidCredentials proves the legitimate
// path is unchanged: a poll carrying valid credentials still populates an auto
// arr's Host/Token and registers it in the shared map.
func TestAuthenticatePopulatesAutoArrWithValidCredentials(t *testing.T) {
	srv := sabHealthOKServer(t)
	s, m := newSABAuthHarness(t)

	const category = "radarr"
	got, err := s.authenticate(category, srv.URL, "valid-token")
	if err != nil {
		t.Fatalf("authenticate with valid creds returned error: %v", err)
	}
	if got.Host != srv.URL || got.Token != "valid-token" {
		t.Fatalf("auto arr not populated: got Host=%q Token=%q", got.Host, got.Token)
	}

	shared := m.Arr().Get(category)
	if shared == nil || shared.Host != srv.URL || shared.Token != "valid-token" {
		t.Fatalf("valid creds not registered in shared map: %+v", shared)
	}
}
