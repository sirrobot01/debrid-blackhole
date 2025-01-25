package server

import (
	"context"
	"encoding/base64"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"net/http"
	"path/filepath"
	"strings"
)

type qbitHandler struct {
	qbit   *shared.QBit
	logger zerolog.Logger
	debug  bool
}

func decodeAuthHeader(header string) (string, string, error) {
	encodedTokens := strings.Split(header, " ")
	if len(encodedTokens) != 2 {
		return "", "", nil
	}
	encodedToken := encodedTokens[1]

	bytes, err := base64.StdEncoding.DecodeString(encodedToken)
	if err != nil {
		return "", "", err
	}

	bearer := string(bytes)

	colonIndex := strings.LastIndex(bearer, ":")
	host := bearer[:colonIndex]
	token := bearer[colonIndex+1:]

	return host, token, nil
}

func (q *qbitHandler) CategoryContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := strings.Trim(r.URL.Query().Get("category"), "")
		if category == "" {
			// Get from form
			_ = r.ParseForm()
			category = r.Form.Get("category")
			if category == "" {
				// Get from multipart form
				_ = r.ParseMultipartForm(32 << 20)
				category = r.FormValue("category")
			}
		}
		ctx := r.Context()
		ctx = context.WithValue(r.Context(), "category", strings.TrimSpace(category))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (q *qbitHandler) authContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, token, err := decodeAuthHeader(r.Header.Get("Authorization"))
		category := r.Context().Value("category").(string)
		a := &arr.Arr{
			Name: category,
		}
		if err == nil {
			a.Host = strings.TrimSpace(host)
			a.Token = strings.TrimSpace(token)
		}
		q.qbit.Arrs.AddOrUpdate(a)
		ctx := context.WithValue(r.Context(), "arr", a)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func HashesCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_hashes := chi.URLParam(r, "hashes")
		var hashes []string
		if _hashes != "" {
			hashes = strings.Split(_hashes, "|")
		}
		if hashes == nil {
			// Get hashes from form
			_ = r.ParseForm()
			hashes = r.Form["hashes"]
		}
		for i, hash := range hashes {
			hashes[i] = strings.TrimSpace(hash)
		}
		ctx := context.WithValue(r.Context(), "hashes", hashes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (q *qbitHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("Ok."))
}

func (q *qbitHandler) handleVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("v4.3.2"))
}

func (q *qbitHandler) handleWebAPIVersion(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("2.7"))
}

func (q *qbitHandler) handlePreferences(w http.ResponseWriter, r *http.Request) {
	preferences := shared.NewAppPreferences()

	preferences.WebUiUsername = q.qbit.Username
	preferences.SavePath = q.qbit.DownloadFolder
	preferences.TempPath = filepath.Join(q.qbit.DownloadFolder, "temp")

	common.JSONResponse(w, preferences, http.StatusOK)
}

func (q *qbitHandler) handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	res := shared.BuildInfo{
		Bitness:    64,
		Boost:      "1.75.0",
		Libtorrent: "1.2.11.0",
		Openssl:    "1.1.1i",
		Qt:         "5.15.2",
		Zlib:       "1.2.11",
	}
	common.JSONResponse(w, res, http.StatusOK)
}

func (q *qbitHandler) shutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	//log all url params
	ctx := r.Context()
	category := ctx.Value("category").(string)
	filter := strings.Trim(r.URL.Query().Get("filter"), "")
	hashes, _ := ctx.Value("hashes").([]string)
	torrents := q.qbit.Storage.GetAll(category, filter, hashes)
	common.JSONResponse(w, torrents, http.StatusOK)
}

func (q *qbitHandler) handleTorrentsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse form based on content type
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			q.logger.Info().Msgf("Error parsing multipart form: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			q.logger.Info().Msgf("Error parsing form: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}

	isSymlink := strings.ToLower(r.FormValue("sequentialDownload")) != "true"
	category := r.FormValue("category")
	atleastOne := false
	ctx = context.WithValue(ctx, "isSymlink", isSymlink)

	// Handle magnet URLs
	if urls := r.FormValue("urls"); urls != "" {
		var urlList []string
		for _, u := range strings.Split(urls, "\n") {
			urlList = append(urlList, strings.TrimSpace(u))
		}
		for _, url := range urlList {
			if err := q.qbit.AddMagnet(ctx, url, category); err != nil {
				q.logger.Info().Msgf("Error adding magnet: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			atleastOne = true
		}
	}

	// Handle torrent files
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		if files := r.MultipartForm.File["torrents"]; len(files) > 0 {
			for _, fileHeader := range files {
				if err := q.qbit.AddTorrent(ctx, fileHeader, category); err != nil {
					q.logger.Info().Msgf("Error adding torrent: %v", err)
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				atleastOne = true
			}
		}
	}

	if !atleastOne {
		http.Error(w, "No valid URLs or torrents provided", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleTorrentsDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	if len(hashes) == 0 {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	for _, hash := range hashes {
		q.qbit.Storage.Delete(hash)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleTorrentsPause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.qbit.PauseTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleTorrentsResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.qbit.ResumeTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleTorrentRecheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	for _, hash := range hashes {
		torrent := q.qbit.Storage.Get(hash)
		if torrent == nil {
			continue
		}
		go q.qbit.RefreshTorrent(torrent)
	}

	w.WriteHeader(http.StatusOK)
}

func (q *qbitHandler) handleCategories(w http.ResponseWriter, r *http.Request) {
	var categories = map[string]shared.TorrentCategory{}
	for _, cat := range q.qbit.Categories {
		path := filepath.Join(q.qbit.DownloadFolder, cat)
		categories[cat] = shared.TorrentCategory{
			Name:     cat,
			SavePath: path,
		}
	}
	common.JSONResponse(w, categories, http.StatusOK)
}

func (q *qbitHandler) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
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

	q.qbit.Categories = append(q.qbit.Categories, name)

	common.JSONResponse(w, nil, http.StatusOK)
}

func (q *qbitHandler) handleTorrentProperties(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	torrent := q.qbit.Storage.Get(hash)
	properties := q.qbit.GetTorrentProperties(torrent)
	common.JSONResponse(w, properties, http.StatusOK)
}

func (q *qbitHandler) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get("hash")
	torrent := q.qbit.Storage.Get(hash)
	if torrent == nil {
		return
	}
	files := q.qbit.GetTorrentFiles(torrent)
	common.JSONResponse(w, files, http.StatusOK)
}

func (q *qbitHandler) handleSetCategory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	category := ctx.Value("category").(string)
	hashes, _ := ctx.Value("hashes").([]string)
	torrents := q.qbit.Storage.GetAll("", "", hashes)
	for _, torrent := range torrents {
		torrent.Category = category
		q.qbit.Storage.AddOrUpdate(torrent)
	}
	common.JSONResponse(w, nil, http.StatusOK)
}

func (q *qbitHandler) handleAddTorrentTags(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	tags := strings.Split(r.FormValue("tags"), ",")
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	torrents := q.qbit.Storage.GetAll("", "", hashes)
	for _, t := range torrents {
		q.qbit.SetTorrentTags(t, tags)
	}
	common.JSONResponse(w, nil, http.StatusOK)
}

func (q *qbitHandler) handleRemoveTorrentTags(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	hashes, _ := ctx.Value("hashes").([]string)
	tags := strings.Split(r.FormValue("tags"), ",")
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	torrents := q.qbit.Storage.GetAll("", "", hashes)
	for _, torrent := range torrents {
		q.qbit.RemoveTorrentTags(torrent, tags)

	}
	common.JSONResponse(w, nil, http.StatusOK)
}

func (q *qbitHandler) handleGetTags(w http.ResponseWriter, r *http.Request) {
	common.JSONResponse(w, q.qbit.Tags, http.StatusOK)
}

func (q *qbitHandler) handleCreateTags(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}
	tags := strings.Split(r.FormValue("tags"), ",")
	for i, tag := range tags {
		tags[i] = strings.TrimSpace(tag)
	}
	q.qbit.AddTags(tags)
	common.JSONResponse(w, nil, http.StatusOK)
}
