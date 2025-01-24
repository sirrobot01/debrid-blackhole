package server

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (u *uiHandler) Routes(r chi.Router) http.Handler {
	r.Group(func(r chi.Router) {
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
