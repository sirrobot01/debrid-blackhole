package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
)

type AddRequest struct {
	Url        string   `json:"url"`
	Arr        string   `json:"arr"`
	File       string   `json:"file"`
	NotSymlink bool     `json:"notSymlink"`
	Content    string   `json:"content"`
	Seasons    []string `json:"seasons"`
	Episodes   []string `json:"episodes"`
}

type ArrResponse struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type ContentResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	ArrID string `json:"arr"`
}

type RepairRequest struct {
	ArrName  string   `json:"arr"`
	MediaIds []string `json:"mediaIds"`
	Async    bool     `json:"async"`
}

//go:embed templates/*
var content embed.FS

type UIHandler struct {
	qbit   *shared.QBit
	logger zerolog.Logger
	debug  bool
}

var templates *template.Template

func init() {
	templates = template.Must(template.ParseFS(
		content,
		"templates/layout.html",
		"templates/index.html",
		"templates/download.html",
		"templates/repair.html",
		"templates/config.html",
	))
}

func (u *UIHandler) IndexHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "index",
		"Title": "Torrents",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *UIHandler) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "download",
		"Title": "Download",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *UIHandler) RepairHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "repair",
		"Title": "Repair",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *UIHandler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "config",
		"Title": "Config",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *UIHandler) handleGetArrs(w http.ResponseWriter, r *http.Request) {
	request.JSONResponse(w, u.qbit.Arrs.GetAll(), http.StatusOK)
}

func (u *UIHandler) handleAddContent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	results := make([]*ImportRequest, 0)
	errs := make([]string, 0)

	arrName := r.FormValue("arr")
	notSymlink := r.FormValue("notSymlink") == "true"

	_arr := u.qbit.Arrs.Get(arrName)
	if _arr == nil {
		_arr = arr.NewArr(arrName, "", "", arr.Sonarr)
	}

	// Handle URLs
	if urls := r.FormValue("urls"); urls != "" {
		var urlList []string
		for _, u := range strings.Split(urls, "\n") {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				urlList = append(urlList, trimmed)
			}
		}

		for _, url := range urlList {
			importReq := NewImportRequest(url, _arr, !notSymlink)
			err := importReq.Process(u.qbit)
			if err != nil {
				errs = append(errs, fmt.Sprintf("URL %s: %v", url, err))
				continue
			}
			results = append(results, importReq)
		}
	}

	// Handle torrent/magnet files
	if files := r.MultipartForm.File["files"]; len(files) > 0 {
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				errs = append(errs, fmt.Sprintf("Failed to open file %s: %v", fileHeader.Filename, err))
				continue
			}

			magnet, err := utils.GetMagnetFromFile(file, fileHeader.Filename)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Failed to parse torrent file %s: %v", fileHeader.Filename, err))
				continue
			}

			importReq := NewImportRequest(magnet.Link, _arr, !notSymlink)
			err = importReq.Process(u.qbit)
			if err != nil {
				errs = append(errs, fmt.Sprintf("File %s: %v", fileHeader.Filename, err))
				continue
			}
			results = append(results, importReq)
		}
	}

	request.JSONResponse(w, struct {
		Results []*ImportRequest `json:"results"`
		Errors  []string         `json:"errors,omitempty"`
	}{
		Results: results,
		Errors:  errs,
	}, http.StatusOK)
}

func (u *UIHandler) handleCheckCached(w http.ResponseWriter, r *http.Request) {
	_hashes := r.URL.Query().Get("hash")
	if _hashes == "" {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	hashes := strings.Split(_hashes, ",")
	if len(hashes) == 0 {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	db := r.URL.Query().Get("debrid")
	var deb debrid.Service
	if db == "" {
		// use the first debrid
		deb = u.qbit.Debrid.Get()
	} else {
		deb = u.qbit.Debrid.GetByName(db)
	}
	if deb == nil {
		http.Error(w, "Invalid debrid", http.StatusBadRequest)
		return
	}
	res := deb.IsAvailable(hashes)
	result := make(map[string]bool)
	for _, h := range hashes {
		_, exists := res[h]
		result[h] = exists
	}
	request.JSONResponse(w, result, http.StatusOK)
}

func (u *UIHandler) handleRepairMedia(w http.ResponseWriter, r *http.Request) {
	var req RepairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_arr := u.qbit.Arrs.Get(req.ArrName)
	if _arr == nil {
		http.Error(w, "No Arrs found to repair", http.StatusNotFound)
		return
	}

	if req.Async {
		go func() {
			if err := u.qbit.Repair.Repair([]*arr.Arr{_arr}, req.MediaIds); err != nil {
				u.logger.Error().Err(err).Msg("Failed to repair media")
			}
		}()
		request.JSONResponse(w, "Repair process started", http.StatusOK)
		return
	}

	if err := u.qbit.Repair.Repair([]*arr.Arr{_arr}, req.MediaIds); err != nil {
		http.Error(w, fmt.Sprintf("Failed to repair: %v", err), http.StatusInternalServerError)
		return

	}

	request.JSONResponse(w, "Repair completed", http.StatusOK)
}

func (u *UIHandler) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	v := version.GetInfo()
	request.JSONResponse(w, v, http.StatusOK)
}

func (u *UIHandler) handleGetTorrents(w http.ResponseWriter, r *http.Request) {
	request.JSONResponse(w, u.qbit.Storage.GetAll("", "", nil), http.StatusOK)
}

func (u *UIHandler) handleDeleteTorrent(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		http.Error(w, "No hash provided", http.StatusBadRequest)
		return
	}
	u.qbit.Storage.Delete(hash)
	w.WriteHeader(http.StatusOK)
}

func (u *UIHandler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.GetConfig()
	arrCfgs := make([]config.Arr, 0)
	for _, a := range u.qbit.Arrs.GetAll() {
		arrCfgs = append(arrCfgs, config.Arr{Host: a.Host, Name: a.Name, Token: a.Token})
	}
	cfg.Arrs = arrCfgs
	request.JSONResponse(w, cfg, http.StatusOK)
}
