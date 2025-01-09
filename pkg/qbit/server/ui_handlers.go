package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"goBlack/common"
	"goBlack/pkg/arr"
	"goBlack/pkg/debrid"
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
	ArrName string `json:"arr"`
	TVIds   string `json:"tvIds"`
	Async   bool   `json:"async"`
}

//go:embed static/index.html
var content embed.FS

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(content, "static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetArrs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, s.qbit.Arrs.GetAll(), http.StatusOK)
}

func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	arrName := r.URL.Query().Get("arr")
	_arr := s.qbit.Arrs.Get(arrName)
	if _arr == nil {
		http.Error(w, "Invalid arr", http.StatusBadRequest)
		return
	}
	contents, _ := _arr.GetMedia("")
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, contents, http.StatusOK)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	// arrName := r.URL.Query().Get("arr")
	term := r.URL.Query().Get("term")
	results, err := arr.SearchTMDB(term)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, results.Results, http.StatusOK)
}

func (s *Server) handleSeasons(w http.ResponseWriter, r *http.Request) {
	// arrId := r.URL.Query().Get("arrId")
	// contentId := chi.URLParam(r, "contentId")
	seasons := []string{"Season 1", "Season 2", "Season 3", "Season 4", "Season 5"}
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, seasons, http.StatusOK)
}

func (s *Server) handleEpisodes(w http.ResponseWriter, r *http.Request) {
	// arrId := r.URL.Query().Get("arrId")
	// contentId := chi.URLParam(r, "contentId")
	// seasonIds := strings.Split(r.URL.Query().Get("seasons"), ",")
	episodes := []string{"Episode 1", "Episode 2", "Episode 3", "Episode 4", "Episode 5"}
	w.Header().Set("Content-Type", "application/json")
	common.JSONResponse(w, episodes, http.StatusOK)
}

func (s *Server) handleAddContent(w http.ResponseWriter, r *http.Request) {
	var req AddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_arr := s.qbit.Arrs.Get(req.Arr)
	if _arr == nil {
		_arr = arr.NewArr(req.Arr, "", "", arr.Sonarr)
	}
	importReq := NewImportRequest(req.Url, _arr, !req.NotSymlink)
	err := importReq.Process(s)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.JSONResponse(w, importReq, http.StatusOK)
}

func (s *Server) handleCheckCached(w http.ResponseWriter, r *http.Request) {
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
		deb = s.qbit.Debrid.Get()
	} else {
		deb = s.qbit.Debrid.GetByName(db)
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

func (s *Server) handleRepair(w http.ResponseWriter, r *http.Request) {
	var req RepairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tvids := []string{""}
	if req.TVIds != "" {
		tvids = strings.Split(req.TVIds, ",")
	}

	_arr := s.qbit.Arrs.Get(req.ArrName)
	arrs := make([]*arr.Arr, 0)
	if _arr != nil {
		arrs = append(arrs, _arr)
	} else {
		arrs = s.qbit.Arrs.GetAll()
	}

	if len(arrs) == 0 {
		http.Error(w, "No arrays found to repair", http.StatusNotFound)
		return
	}

	if req.Async {
		for _, a := range arrs {
			for _, tvId := range tvids {
				go a.Repair(tvId)
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
