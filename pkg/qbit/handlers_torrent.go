package qbit

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
)

func (q *QBit) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	//log all url params
	ctx := r.Context()
	category := strings.Trim(r.URL.Query().Get("category"), "")
	filter := strings.Trim(r.URL.Query().Get("filter"), "")
	hashes, _ := ctx.Value("hashes").([]string)
	torrents := q.storage.GetAll(category, filter, hashes)
	JSONResponse(w, torrents, http.StatusOK)
}

func (q *QBit) handleTorrentsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	contentType := strings.Split(r.Header.Get("Content-Type"), ";")[0]
	switch contentType {
	case "multipart/form-data":
		err := r.ParseMultipartForm(32 << 20) // 32MB max memory
		if err != nil {
			q.logger.Printf("Error parsing form: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case "application/x-www-form-urlencoded":
		err := r.ParseForm()
		if err != nil {
			q.logger.Printf("Error parsing form: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	isSymlink := strings.ToLower(r.FormValue("sequentialDownload")) != "true"
	q.logger.Printf("isSymlink: %v\n", isSymlink)
	urls := r.FormValue("urls")
	category := r.FormValue("category")

	var urlList []string
	if urls != "" {
		urlList = strings.Split(urls, "\n")
	}

	ctx = context.WithValue(ctx, "isSymlink", isSymlink)

	for _, url := range urlList {
		if err := q.AddMagnet(ctx, url, category); err != nil {
			q.logger.Printf("Error adding magnet: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if contentType == "multipart/form-data" {
		files := r.MultipartForm.File["torrents"]
		for _, fileHeader := range files {
			if err := q.AddTorrent(ctx, fileHeader, category); err != nil {
				q.logger.Printf("Error adding torrent: %v\n", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleTorrentsDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	if len(hashes) == 0 {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	for _, hash := range hashes {
		q.storage.Delete(hash)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleTorrentsPause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.PauseTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleTorrentsResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.ResumeTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleTorrentRecheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.RefreshTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *QBit) handleCategories(w http.ResponseWriter, r *http.Request) {
	var categories = map[string]TorrentCategory{}
	for _, cat := range q.Categories {
		path := filepath.Join(q.DownloadFolder, cat)
		categories[cat] = TorrentCategory{
			Name:     cat,
			SavePath: path,
		}
	}
	JSONResponse(w, categories, http.StatusOK)
}

func (q *QBit) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
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

	q.Categories = append(q.Categories, name)

	JSONResponse(w, nil, http.StatusOK)
}

func (q *QBit) handleTorrentProperties(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	torrent := q.storage.Get(hash)
	properties := q.GetTorrentProperties(torrent)
	JSONResponse(w, properties, http.StatusOK)
}
