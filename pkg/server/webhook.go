package server

import (
	"cmp"
	"crypto/subtle"
	"net/http"
	"strings"

	json "github.com/bytedance/sonic"
	"github.com/go-chi/chi/v5"

	"github.com/sirrobot01/decypharr/internal/config"
)

// webhookRoutes returns the webhook router, mounted at /webhooks.
//
// Every route here is authenticated. These endpoints launch repair work, which
// can delete files, so an unauthenticated caller must never reach them. The
// Tautulli route was previously registered on the parent router, outside the
// auth group, and required no credentials at all.
func (s *Server) webhookRoutes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.webhookAuthMiddleware)
	r.Post("/tautulli", s.handleTautulli)
	return r
}

// webhookAuthMiddleware rejects unauthenticated webhook calls with a JSON 401.
func (s *Server) webhookAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.webhookAuthorized(r) {
			s.sendJSONError(w,
				"Authentication required. Provide the API token via the Authorization header "+
					"(Bearer <token>), the X-API-Token header, or the ?token= query parameter.",
				http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// webhookAuthorized reports whether a webhook request carries valid
// credentials. The credential is the server's existing API token; no new auth
// scheme is introduced.
//
// Besides the standard `Authorization: Bearer|Token <token>` header, the same
// token is accepted from an `X-API-Token` header or a `?token=` / `?apikey=`
// query parameter. Notification agents vary in what they can attach to an
// outgoing request, and without a mechanism they can actually use, requiring
// auth here would simply break every existing sender.
//
// use_auth == false preserves the existing global behaviour: the whole server
// is intentionally open in that mode, and this endpoint is not special-cased
// around it.
func (s *Server) webhookAuthorized(r *http.Request) bool {
	cfg := config.Get()
	if !cfg.UseAuth {
		return true
	}
	// GetAuth also lazily loads auth.json, so it must run before NeedsAuth.
	auth := cfg.GetAuth()
	if cfg.NeedsAuth() {
		// Auth is enabled but no credentials exist yet: fail closed.
		return false
	}
	if s.isValidAPIToken(r) {
		return true
	}
	if auth == nil || auth.APIToken == "" {
		return false
	}
	supplied := cmp.Or(
		strings.TrimSpace(r.Header.Get("X-API-Token")),
		strings.TrimSpace(r.URL.Query().Get("token")),
		strings.TrimSpace(r.URL.Query().Get("apikey")),
	)
	if supplied == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(auth.APIToken)) == 1
}

// handleTautulli handles webhooks from Tautulli. The payload must carry a
// tvdb/tmdb id (or a generic media_id): the repair system then runs a targeted
// recheck against that specific media — the v2 equivalent of v1's
// "media-id-scoped repair job".
//
// A payload with no media id is rejected with 400. It previously fell through
// to svc.RunNow(...), i.e. a full library sweep using the operator's configured
// repair settings, so a single untargeted notification could mass-delete.
// Nothing about a Tautulli notification implies "sweep the entire library", so
// the untargeted case is an explicit client error — and it is checked before
// the repair service is looked up, so no path remains from an untargeted
// payload to a sweep.
func (s *Server) handleTautulli(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Topic   string `json:"topic"`
		Arr     string `json:"arr,omitempty"`
		MediaID string `json:"media_id,omitempty"`
		TvdbID  string `json:"tvdb_id,omitempty"`
		TmdbID  string `json:"tmdb_id,omitempty"`
		Fix     bool   `json:"fix,omitempty"`
	}
	if err := json.ConfigDefault.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.Error().Err(err).Msg("Failed to parse webhook body")
		http.Error(w, "Failed to parse webhook body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if payload.Topic != "tautulli" {
		http.Error(w, "Invalid topic", http.StatusBadRequest)
		return
	}

	mediaID := strings.TrimSpace(cmp.Or(payload.MediaID, payload.TmdbID, payload.TvdbID))
	if mediaID == "" {
		// No targeting. Never fall back to a full sweep from a webhook.
		http.Error(w, "media_id (or tmdb_id / tvdb_id) is required", http.StatusBadRequest)
		return
	}

	svc := s.manager.Repair()
	if svc == nil {
		http.Error(w, "Repair service not available", http.StatusServiceUnavailable)
		return
	}

	run, err := svc.RecheckMedia(s.manager.Context(), strings.TrimSpace(payload.Arr), mediaID, payload.Fix)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already running") {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	if run != nil {
		s.logger.Info().
			Str("run_id", run.ID).
			Str("arr", payload.Arr).
			Str("media_id", mediaID).
			Bool("fix", payload.Fix).
			Msg("Tautulli webhook: media recheck triggered")
	}
	w.WriteHeader(http.StatusOK)
}
