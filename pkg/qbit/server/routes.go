package server

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"net/http"
)

func (q *qbitHandler) Routes(r chi.Router) http.Handler {
	r.Route("/api/v2", func(r chi.Router) {
		if q.debug {
			r.Use(middleware.Logger)
		}
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

func (u *uiHandler) Routes(r chi.Router) http.Handler {
	r.Group(func(r chi.Router) {
		if u.debug {
			r.Use(middleware.Logger)
		}
		r.Get("/", u.IndexHandler)
		r.Get("/download", u.DownloadHandler)
		r.Get("/repair", u.RepairHandler)
		r.Get("/config", u.ConfigHandler)
		r.Route("/internal", func(r chi.Router) {
			r.Get("/arrs", u.handleGetArrs)
			r.Post("/add", u.handleAddContent)
			r.Get("/cached", u.handleCheckCached)
			r.Post("/repair", u.handleRepairMedia)
			r.Get("/torrents", u.handleGetTorrents)
			r.Delete("/torrents/{hash}", u.handleDeleteTorrent)
			r.Get("/config", u.handleGetConfig)
			r.Get("/version", u.handleGetVersion)
		})
	})

	return r
}
