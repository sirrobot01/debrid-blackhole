package server

import (
	"cmp"
	"crypto/subtle"
	"net/http"

	json "github.com/bytedance/sonic"
	"github.com/sirrobot01/decypharr/pkg/arr"
)

// handleArrWebhook receives webhooks from Sonarr/Radarr and delegates to the manager
// for ref tracking and optional debrid cleanup.
func (s *Server) handleArrWebhook(w http.ResponseWriter, r *http.Request) {
	var payload arr.WebhookPayload
	if err := json.ConfigDefault.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.Error().Err(err).Str("component", "arr_webhook").Msg("Failed to parse body")
		http.Error(w, "Bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Resolve ARR name: ?arr= query param takes priority over instanceName and X-Arr-Name header.
	arrName := cmp.Or(r.URL.Query().Get("arr"), payload.InstanceName, r.Header.Get("X-Arr-Name"))
	if arrName == "" {
		s.logger.Warn().Str("component", "arr_webhook").Msg("Cannot determine ARR name, rejecting")
		http.Error(w, "Cannot determine ARR name: set ?arr= query param or configure instanceName", http.StatusBadRequest)
		return
	}

	// Authenticate: this route sits outside authMiddleware (Sonarr/Radarr can't do
	// session/basic auth on webhook connections), so the token RegisterArrWebhooks
	// embedded in the registered URL — the ARR's own API token — is the only thing
	// tying a request to the ARR it claims to be from. Nothing else in the payload
	// (instanceName, downloadId, paths) can be trusted; it's an unsigned POST body
	// anyone reaching this port could otherwise forge to trigger deletions.
	a := s.manager.GetArrStorage().Get(arrName)
	if !arrWebhookAuthorized(a, r.URL.Query().Get("token")) {
		s.logger.Warn().Str("component", "arr_webhook").Str("arr", arrName).Msg("Rejecting webhook: unknown ARR or invalid token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Test event – sent by Sonarr/Radarr when saving the webhook connection. The
	// Test button POSTs to the URL exactly as configured, so it still carries the
	// token above and goes through the same auth as any other event.
	if payload.EventType == arr.EventTypeTest {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch payload.EventType {
	case arr.EventTypeDownload:
		s.manager.HandleArrImport(arrName, &payload)
	case arr.EventTypeEpisodeFileDelete, arr.EventTypeMovieFileDelete:
		s.manager.HandleArrDelete(arrName, &payload)
	case arr.EventTypeRename:
		s.manager.HandleArrRename(arrName, &payload)
	case arr.EventTypeSeriesDelete:
		s.manager.HandleArrSeriesDelete(arrName, &payload)
	case arr.EventTypeMovieDelete:
		s.manager.HandleArrMovieDelete(arrName, &payload)
	default:
		s.logger.Debug().
			Str("component", "arr_webhook").
			Str("event_type", payload.EventType).
			Msg("Unhandled event type, ignoring")
	}

	w.WriteHeader(http.StatusOK)
}

// arrWebhookAuthorized reports whether providedToken matches a's own API token —
// the only signal tying a webhook request to the ARR it claims to be from, since
// nothing else in the payload can be trusted. Isolated from handleArrWebhook so
// the comparison itself is unit-testable without an HTTP/Manager harness.
func arrWebhookAuthorized(a *arr.Arr, providedToken string) bool {
	return a != nil && a.Token != "" && subtle.ConstantTimeCompare([]byte(providedToken), []byte(a.Token)) == 1
}
