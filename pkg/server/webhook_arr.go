package server

import (
	"cmp"
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

	// Test event – sent by Sonarr/Radarr when saving the webhook connection.
	if payload.EventType == arr.EventTypeTest {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Resolve ARR name: ?arr= query param takes priority over instanceName and X-Arr-Name header.
	arrName := cmp.Or(r.URL.Query().Get("arr"), payload.InstanceName, r.Header.Get("X-Arr-Name"))
	if arrName == "" {
		s.logger.Warn().Str("component", "arr_webhook").Msg("Cannot determine ARR name, rejecting")
		http.Error(w, "Cannot determine ARR name: set ?arr= query param or configure instanceName", http.StatusBadRequest)
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
