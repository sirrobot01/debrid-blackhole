package qbit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

func newQBitTestManager(t *testing.T) *manager.Manager {
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
	return m
}

// healthOKServer answers 200 to the arr health probe so Arr.Validate()
// completes quickly and deterministically, without any real network
// dependency, when authenticate runs its validation pass.
func healthOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAuthenticateDoesNotWipeAutoArrOnEmptyCredentials is the regression guard
// for the shared-arr corruption bug. When Sonarr/Radarr poll GET /torrents/info
// with blank download-client credentials, authenticate must NOT overwrite an
// already-populated auto arr's Host/Token with empty strings.
//
// Pre-fix, the unconditional `if a.Source == "auto" { a.Host = username;
// a.Token = password }` wiped Host/Token to "" on every empty-cred poll,
// corrupting the shared arr map that other consumers rely on — notably the
// repair service, which then rejects the arr with "arr not configured".
func TestAuthenticateDoesNotWipeAutoArrOnEmptyCredentials(t *testing.T) {
	srv := healthOKServer(t)
	m := newQBitTestManager(t)

	const category = "sonarr"
	seeded := arr.New(category, srv.URL, "seed-token", false, nil, "", string(arr.SourceAuto))
	m.Arr().AddOrUpdate(seeded)

	q := New(m)
	got, err := q.authenticate(category, "", "")
	if err != nil {
		t.Fatalf("authenticate with empty creds returned error: %v", err)
	}
	if got.Host != srv.URL {
		t.Fatalf("Host wiped by empty-cred poll: got %q, want %q", got.Host, srv.URL)
	}
	if got.Token != "seed-token" {
		t.Fatalf("Token wiped by empty-cred poll: got %q, want %q", got.Token, "seed-token")
	}

	// The shared map entry every other consumer reads must stay intact.
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
	srv := healthOKServer(t)
	m := newQBitTestManager(t)

	const category = "radarr"
	q := New(m)
	got, err := q.authenticate(category, srv.URL, "valid-token")
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
