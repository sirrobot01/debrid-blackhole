package qbit

import (
	"goBlack/common"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
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
	err := r.ParseMultipartForm(32 << 20) // 32MB max memory
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["torrents"]
	urls := r.FormValue("urls")
	category := r.FormValue("category")

	if len(files) == 0 && urls == "" {
		http.Error(w, "No torrent provided", http.StatusBadRequest)
		return
	}

	var urlList []string
	if urls != "" {
		urlList = strings.Split(urls, "\n")
	}

	var wg sync.WaitGroup
	for _, url := range urlList {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			magnet, err := common.GetMagnetFromUrl(url)
			if err != nil {
				q.logger.Printf("Error parsing magnet link: %v\n", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_, err = q.Process(magnet, category)
			if err != nil {
				q.logger.Printf("Error processing magnet: %v\n", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}(url)
	}

	for _, fileHeader := range files {
		wg.Add(1)
		go func(fileHeader *multipart.FileHeader) {
			defer wg.Done()
			file, _ := fileHeader.Open()
			defer file.Close()
			var reader io.Reader = file
			magnet, err := common.GetMagnetFromFile(reader, fileHeader.Filename)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				q.logger.Printf("Error reading file: %s", fileHeader.Filename)
				return
			}
			_, err = q.Process(magnet, category)
		}(fileHeader)
	}
	wg.Wait()
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
