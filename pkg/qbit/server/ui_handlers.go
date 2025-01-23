package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
	"html/template"
	"net/http"
	"strings"
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

type uiHandler struct {
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

func (u *uiHandler) IndexHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "index",
		"Title": "Torrents",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *uiHandler) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "download",
		"Title": "Download",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *uiHandler) RepairHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "repair",
		"Title": "Repair",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *uiHandler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "config",
		"Title": "Config",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (u *uiHandler) handleGetArrs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, u.qbit.Arrs.GetAll(), http.StatusOK)
}

func (u *uiHandler) handleAddContent(w http.ResponseWriter, r *http.Request) {
	var req AddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_arr := u.qbit.Arrs.Get(req.Arr)
	if _arr == nil {
		_arr = arr.NewArr(req.Arr, "", "", arr.Sonarr)
	}
	importReq := NewImportRequest(req.Url, _arr, !req.NotSymlink)
	err := importReq.Process(u.qbit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.JSONResponse(w, importReq, http.StatusOK)
}

func (u *uiHandler) handleCheckCached(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
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
	common.JSONResponse(w, result, http.StatusOK)
}

func (u *uiHandler) handleRepairMedia(w http.ResponseWriter, r *http.Request) {
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

	mediaIds := req.MediaIds
	if len(mediaIds) == 0 {
		mediaIds = []string{""}
	}

	if req.Async {
		for _, tvId := range mediaIds {
			go func() {
				err := _arr.Repair(tvId)
				if err != nil {
					u.logger.Info().Msgf("Failed to repair: %v", err)
				}
			}()
		}
		common.JSONResponse(w, "Repair process started", http.StatusOK)
		return
	}

	var errs []error
	for _, tvId := range mediaIds {
		if err := _arr.Repair(tvId); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		combinedErr := errors.Join(errs...)
		http.Error(w, fmt.Sprintf("Failed to repair: %v", combinedErr), http.StatusInternalServerError)
		return
	}

	common.JSONResponse(w, "Repair completed", http.StatusOK)
}

func (u *uiHandler) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	v := version.GetInfo()
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, v, http.StatusOK)
}

func (u *uiHandler) handleGetTorrents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, u.qbit.Storage.GetAll("", "", nil), http.StatusOK)
}

func (u *uiHandler) handleDeleteTorrent(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		http.Error(w, "No hash provided", http.StatusBadRequest)
		return
	}
	u.qbit.Storage.Delete(hash)
	w.WriteHeader(http.StatusOK)
}

func (u *uiHandler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	config := common.CONFIG
	arrCfgs := make([]common.ArrConfig, 0)
	for _, a := range u.qbit.Arrs.GetAll() {
		arrCfgs = append(arrCfgs, common.ArrConfig{Host: a.Host, Name: a.Name, Token: a.Token})
	}
	config.Arrs = arrCfgs
	common.JSONResponse(w, config, http.StatusOK)
}
