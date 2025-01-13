package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
	"github.com/sirrobot01/debrid-blackhole/pkg/version"
	"html/template"
	"log"
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
	ArrName string `json:"arr"`
	TVIds   string `json:"tvIds"`
	Async   bool   `json:"async"`
}

//go:embed templates/*
var content embed.FS

type uiHandler struct {
	qbit   *shared.QBit
	logger *log.Logger
	debug  bool
}

var templates *template.Template

func init() {
	currentDir := "pkg/qbit/server"
	templates = template.Must(template.ParseFiles(
		currentDir+"/templates/layout.html",
		currentDir+"/templates/index.html",
		currentDir+"/templates/download.html",
		currentDir+"/templates/repair.html",
		currentDir+"/templates/config.html",
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
	tvids := []string{""}
	if req.TVIds != "" {
		tvids = strings.Split(req.TVIds, ",")
	}

	_arr := u.qbit.Arrs.Get(req.ArrName)
	arrs := make([]*arr.Arr, 0)
	if _arr != nil {
		arrs = append(arrs, _arr)
	} else {
		arrs = u.qbit.Arrs.GetAll()
	}

	if len(arrs) == 0 {
		http.Error(w, "No arrays found to repair", http.StatusNotFound)
		return
	}

	if req.Async {
		for _, a := range arrs {
			for _, tvId := range tvids {
				go func() {
					err := a.Repair(tvId)
					if err != nil {
						u.logger.Printf("Failed to repair: %v", err)
					}
				}()
			}
		}
		common.JSONResponse(w, "Repair process started", http.StatusOK)
		return
	}

	var errs []error
	for _, a := range arrs {
		for _, tvId := range tvids {
			if err := a.Repair(tvId); err != nil {
				errs = append(errs, err)
			}
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
	common.JSONResponse(w, common.CONFIG, http.StatusOK)
}
