package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
)

const testAPIToken = "test-api-token"

// newWebhookTestServer builds the smallest Server able to serve the webhook
// router, with auth enabled and a known API token.
//
// manager is deliberately left nil. Both behaviours under test must be decided
// before the repair service is ever looked up, so a nil manager is not a
// limitation of the test — it is the assertion. Any path that reaches
// s.manager.Repair() is by definition a path that reached repair work.
func newWebhookTestServer(t *testing.T) *Server {
	t.Helper()
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	cfg := config.Get()
	cfg.UseAuth = true
	cfg.Auth = &config.Auth{
		Username: "user",
		Password: "pass",
		APIToken: testAPIToken,
	}

	return &Server{logger: logger.New("webhook-test")}
}

func postWebhook(t *testing.T, s *Server, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	s.webhookRoutes().ServeHTTP(recorder, req)
	return recorder
}

// A targetless payload — valid JSON, correct topic, no media id. This is the
// shape that previously fell through to a full repair sweep.
const targetlessPayload = `{"topic":"tautulli","fix":true}`

// TestTautulliWebhookRequiresAuth pins that the webhook is not reachable
// without credentials. It was registered on the parent router, outside the auth
// group, so any unauthenticated caller could invoke it.
func TestTautulliWebhookRequiresAuth(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		headers map[string]string
	}{
		{"no credentials at all", "/tautulli", nil},
		{"wrong bearer token", "/tautulli", map[string]string{"Authorization": "Bearer not-the-token"}},
		{"wrong X-API-Token", "/tautulli", map[string]string{"X-API-Token": "not-the-token"}},
		{"wrong token query parameter", "/tautulli?token=not-the-token", nil},
		{"empty token query parameter", "/tautulli?token=", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebhookTestServer(t)
			recorder := postWebhook(t, s, tc.target, targetlessPayload, tc.headers)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestTautulliWebhookAcceptsValidCredentials pins that the accepted credential
// forms still work. Reaching 400 (the targetless rejection below) proves the
// request passed authentication rather than being turned away at the door.
func TestTautulliWebhookAcceptsValidCredentials(t *testing.T) {
	cases := []struct {
		name    string
		target  string
		headers map[string]string
	}{
		{"bearer header", "/tautulli", map[string]string{"Authorization": "Bearer " + testAPIToken}},
		{"token header", "/tautulli", map[string]string{"Authorization": "Token " + testAPIToken}},
		{"X-API-Token header", "/tautulli", map[string]string{"X-API-Token": testAPIToken}},
		{"token query parameter", "/tautulli?token=" + testAPIToken, nil},
		{"apikey query parameter", "/tautulli?apikey=" + testAPIToken, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newWebhookTestServer(t)
			recorder := postWebhook(t, s, tc.target, targetlessPayload, tc.headers)
			if recorder.Code == http.StatusUnauthorized {
				t.Fatalf("valid credentials rejected with 401; body=%q", recorder.Body.String())
			}
		})
	}
}

// TestTautulliWebhookRejectsTargetlessPayload pins that a payload carrying no
// media id is a client error, not an instruction to sweep the whole library.
//
// It previously fell through to svc.RunNow(...) — a full sweep using the
// operator's configured repair settings, which can delete. Nothing about a
// Tautulli notification implies "sweep everything".
func TestTautulliWebhookRejectsTargetlessPayload(t *testing.T) {
	for _, body := range []string{
		`{"topic":"tautulli","fix":true}`,
		`{"topic":"tautulli","fix":false}`,
		`{"topic":"tautulli","arr":"radarr","fix":true}`,
		`{"topic":"tautulli","media_id":"   ","fix":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			s := newWebhookTestServer(t)
			recorder := postWebhook(t, s, "/tautulli", body,
				map[string]string{"Authorization": "Bearer " + testAPIToken})
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestTautulliWebhookAuthDisabledStaysOpen pins that use_auth=false is
// unchanged. The whole server is intentionally open in that mode and this
// endpoint is not special-cased around it.
func TestTautulliWebhookAuthDisabledStaysOpen(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().UseAuth = false

	s := &Server{logger: logger.New("webhook-test")}
	recorder := postWebhook(t, s, "/tautulli", targetlessPayload, nil)
	if recorder.Code == http.StatusUnauthorized {
		t.Fatal("use_auth=false must not require webhook credentials")
	}
}
