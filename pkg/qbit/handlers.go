package qbit

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (q *QBit) AddRoutes(r chi.Router) http.Handler {
	r.Route("/api/v2", func(r chi.Router) {
		r.Post("/auth/login", q.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(q.authMiddleware)
			r.Route("/torrents", func(r chi.Router) {
				r.Use(HashesCtx)
				r.Get("/info", q.handleTorrentsInfo)
				r.Post("/add", q.handleTorrentsAdd)
				r.Post("/delete", q.handleTorrentsDelete)
				r.Get("/categories", q.handleCategories)
				r.Post("/createCategory", q.handleCreateCategory)

				r.Get("/pause", q.handleTorrentsPause)
				r.Get("/resume", q.handleTorrentsResume)
				r.Get("/recheck", q.handleTorrentRecheck)
				r.Get("/properties", q.handleTorrentProperties)
			})

			r.Route("/app", func(r chi.Router) {
				r.Get("/version", q.handleVersion)
				r.Get("/webapiVersion", q.handleWebAPIVersion)
				r.Get("/preferences", q.handlePreferences)
				r.Get("/buildInfo", q.handleBuildInfo)
				r.Get("/shutdown", q.shutdown)
			})
		})

	})
	return r
}
