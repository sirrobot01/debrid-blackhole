package server

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (s *Server) Routes(r chi.Router) http.Handler {
	r.Route("/api/v2", func(r chi.Router) {
		r.Use(s.CategoryContext)
		r.Post("/auth/login", s.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(s.authContext)
			r.Route("/torrents", func(r chi.Router) {
				r.Use(HashesCtx)
				r.Get("/info", s.handleTorrentsInfo)
				r.Post("/add", s.handleTorrentsAdd)
				r.Post("/delete", s.handleTorrentsDelete)
				r.Get("/categories", s.handleCategories)
				r.Post("/createCategory", s.handleCreateCategory)

				r.Get("/pause", s.handleTorrentsPause)
				r.Get("/resume", s.handleTorrentsResume)
				r.Get("/recheck", s.handleTorrentRecheck)
				r.Get("/properties", s.handleTorrentProperties)
				r.Get("/files", s.handleTorrentFiles)
			})

			r.Route("/app", func(r chi.Router) {
				r.Get("/version", s.handleVersion)
				r.Get("/webapiVersion", s.handleWebAPIVersion)
				r.Get("/preferences", s.handlePreferences)
				r.Get("/buildInfo", s.handleBuildInfo)
				r.Get("/shutdown", s.shutdown)
			})
		})

	})
	r.Get("/", s.handleHome)
	r.Route("/internal", func(r chi.Router) {
		r.Get("/arrs", s.handleGetArrs)
		r.Get("/content", s.handleContent)
		r.Get("/seasons/{contentId}", s.handleSeasons)
		r.Get("/episodes/{contentId}", s.handleEpisodes)
		r.Post("/add", s.handleAddContent)
		r.Get("/search", s.handleSearch)
	})
	return r
}
