package server

import (
	"embed"
	"encoding/json"
	"goBlack/common"
	"goBlack/pkg/arr"
	"html/template"
	"net/http"
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
	contents := _arr.GetContents()
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
