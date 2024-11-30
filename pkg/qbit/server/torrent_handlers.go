package server

import (
	"context"
	"goBlack/common"
	"goBlack/pkg/qbit/shared"
	"net/http"
	"path/filepath"
	"strings"
)

func (s *Server) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	//log all url params
	ctx := r.Context()
	category := ctx.Value("category").(string)
	filter := strings.Trim(r.URL.Query().Get("filter"), "")
	hashes, _ := ctx.Value("hashes").([]string)
	torrents := s.qbit.Storage.GetAll(category, filter, hashes)
	common.JSONResponse(w, torrents, http.StatusOK)
}

func (s *Server) handleTorrentsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	switch contentType {
	case "multipart/form-data":
		err := r.ParseMultipartForm(32 << 20) // 32MB max memory
		if err != nil {
			s.logger.Printf("Error parsing form: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case "application/x-www-form-urlencoded":
		err := r.ParseForm()
		if err != nil {
			s.logger.Printf("Error parsing form: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	isSymlink := strings.ToLower(r.FormValue("sequentialDownload")) != "true"
	s.logger.Printf("isSymlink: %v\n", isSymlink)
	urls := r.FormValue("urls")
	category := r.FormValue("category")

	var urlList []string
	if urls != "" {
		urlList = strings.Split(urls, "\n")
	}

	ctx = context.WithValue(ctx, "isSymlink", isSymlink)

	for _, url := range urlList {
		if err := s.qbit.AddMagnet(ctx, url, category); err != nil {
			s.logger.Printf("Error adding magnet: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if contentType == "multipart/form-data" {
		files := r.MultipartForm.File["torrents"]
		for _, fileHeader := range files {
			if err := s.qbit.AddTorrent(ctx, fileHeader, category); err != nil {
				s.logger.Printf("Error adding torrent: %v\n", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTorrentsDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	if len(hashes) == 0 {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	for _, hash := range hashes {
		s.qbit.Storage.Delete(hash)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTorrentsPause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := s.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go s.qbit.PauseTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTorrentsResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := s.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go s.qbit.ResumeTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTorrentRecheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := s.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go s.qbit.RefreshTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	var categories = map[string]shared.TorrentCategory{}
	for _, cat := range s.qbit.Categories {
		path := filepath.Join(s.qbit.DownloadFolder, cat)
		categories[cat] = shared.TorrentCategory{
			Name:     cat,
			SavePath: path,
		}
	}
	common.JSONResponse(w, categories, http.StatusOK)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	name := r.Form.Get("category")
	if name == "" {
		http.Error(w, "No name provided", http.StatusBadRequest)
		return
	}

	s.qbit.Categories = append(s.qbit.Categories, name)

	common.JSONResponse(w, nil, http.StatusOK)
}

func (s *Server) handleTorrentProperties(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	torrent := s.qbit.Storage.Get(hash)
	properties := s.qbit.GetTorrentProperties(torrent)
	common.JSONResponse(w, properties, http.StatusOK)
}

func (s *Server) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	torrent := s.qbit.Storage.Get(hash)
	if torrent == nil {
		return
	}
	files := s.qbit.GetTorrentFiles(torrent)
	common.JSONResponse(w, files, http.StatusOK)
}
