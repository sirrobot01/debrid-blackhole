package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sirrobot01/decypharr/internal/utils"
)

func (s *Server) handlePurgeLocalPreview(w http.ResponseWriter, r *http.Request) {
	entries, err := s.manager.GetUnmanagedEntries()
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to get unmanaged entries")
		http.Error(w, "Failed to scan unmanaged entries", http.StatusInternalServerError)
		return
	}

	type entryInfo struct {
		InfoHash string `json:"infohash"`
		Name     string `json:"name"`
		Size     int64  `json:"size"`
	}

	items := make([]entryInfo, 0, len(entries))
	for _, e := range entries {
		items = append(items, entryInfo{
			InfoHash: e.InfoHash,
			Name:     e.Name,
			Size:     e.Size,
		})
	}

	utils.JSONResponse(w, map[string]interface{}{
		"count":   len(items),
		"entries": items,
	}, http.StatusOK)
}

func (s *Server) handlePurgeLocalExecute(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.manager.PurgeUnmanagedEntries()
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to purge unmanaged entries")
		http.Error(w, "Failed to purge unmanaged entries", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"deleted": deleted,
	}, http.StatusOK)
}

func (s *Server) handlePurgeProviderPreview(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "name")
	if provider == "" {
		http.Error(w, "Provider name required", http.StatusBadRequest)
		return
	}

	torrents, err := s.manager.GetUnmanagedProviderTorrents(provider)
	if err != nil {
		s.logger.Error().Err(err).Str("provider", provider).Msg("Failed to get unmanaged provider torrents")
		http.Error(w, "Failed to scan provider: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type torrentInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	items := make([]torrentInfo, 0, len(torrents))
	for _, t := range torrents {
		items = append(items, torrentInfo{
			ID:   t.Id,
			Name: t.Name,
			Size: t.Size,
		})
	}

	utils.JSONResponse(w, map[string]interface{}{
		"count":    len(items),
		"torrents": items,
	}, http.StatusOK)
}

func (s *Server) handlePurgeProviderExecute(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "name")
	if provider == "" {
		http.Error(w, "Provider name required", http.StatusBadRequest)
		return
	}

	deleted, err := s.manager.PurgeUnmanagedProviderTorrents(provider)
	if err != nil {
		s.logger.Error().Err(err).Str("provider", provider).Msg("Failed to purge provider torrents")
		http.Error(w, "Failed to purge provider: "+err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"deleted": deleted,
	}, http.StatusOK)
}
