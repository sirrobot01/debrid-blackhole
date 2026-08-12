package server

import (
	"testing"

	"github.com/sirrobot01/decypharr/pkg/arr"
)

// arrWebhookAuthorized is the entire trust boundary for handleArrWebhook: this
// route sits outside authMiddleware, so a wrong answer here means anyone who can
// reach the port can impersonate any configured ARR and trigger debrid deletions
// (via HandleArrDelete's DownloadId fallback) just by guessing its name.
func TestArrWebhookAuthorized(t *testing.T) {
	configured := arr.New("radarr", "http://radarr:7878", "real-token-123", false, false, nil, "", "manual")

	cases := []struct {
		name  string
		a     *arr.Arr
		token string
		want  bool
	}{
		{"correct token", configured, "real-token-123", true},
		{"wrong token", configured, "guessed-token", false},
		{"empty token", configured, "", false},
		{"unknown arr (nil lookup)", nil, "real-token-123", false},
		{"unknown arr with no token guess", nil, "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := arrWebhookAuthorized(c.a, c.token); got != c.want {
				t.Errorf("arrWebhookAuthorized(%v, %q) = %v, want %v", c.a, c.token, got, c.want)
			}
		})
	}
}

// An ARR without a configured token can never have had a webhook registered for
// it (RegisterArrWebhooks skips those), so any request claiming to be it must be
// rejected outright — there is no legitimate token to compare against.
func TestArrWebhookAuthorized_NoTokenConfigured(t *testing.T) {
	untokened := arr.New("sonarr", "http://sonarr:8989", "", false, false, nil, "", "manual")
	if arrWebhookAuthorized(untokened, "") {
		t.Error("an ARR with no configured token must never authorize a webhook request")
	}
	if arrWebhookAuthorized(untokened, "anything") {
		t.Error("an ARR with no configured token must never authorize a webhook request")
	}
}
