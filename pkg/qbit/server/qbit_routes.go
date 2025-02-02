package server

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (q *QbitHandler) Routes(r chi.Router) http.Handler {
	r.Route("/api/v2", func(r chi.Router) {
		//if q.debug {
		//	r.Use(middleware.Logger)
		//}
		r.Use(q.CategoryContext)
		r.Post("/auth/login", q.handleLogin)

		r.Group(func(r chi.Router) {
			r.Use(q.authContext)
			r.Route("/torrents", func(r chi.Router) {
				r.Use(HashesCtx)
				r.Get("/info", q.handleTorrentsInfo)
				r.Post("/add", q.handleTorrentsAdd)
				r.Post("/delete", q.handleTorrentsDelete)
				r.Get("/categories", q.handleCategories)
				r.Post("/createCategory", q.handleCreateCategory)
				r.Post("/setCategory", q.handleSetCategory)
				r.Post("/addTags", q.handleAddTorrentTags)
				r.Post("/removeTags", q.handleRemoveTorrentTags)
				r.Post("/createTags", q.handleCreateTags)
				r.Get("/tags", q.handleGetTags)
				r.Get("/pause", q.handleTorrentsPause)
				r.Get("/resume", q.handleTorrentsResume)
				r.Get("/recheck", q.handleTorrentRecheck)
				r.Get("/properties", q.handleTorrentProperties)
				r.Get("/files", q.handleTorrentFiles)
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
