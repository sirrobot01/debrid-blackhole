package web

import (
	"embed"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/gorilla/sessions"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/qbit"
	"github.com/sirrobot01/decypharr/pkg/service"
	"golang.org/x/crypto/bcrypt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/version"
)

var restartFunc func()

// SetRestartFunc allows setting a callback to restart services
func SetRestartFunc(fn func()) {
	restartFunc = fn
}

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
	ArrName     string   `json:"arr"`
	MediaIds    []string `json:"mediaIds"`
	Async       bool     `json:"async"`
	AutoProcess bool     `json:"autoProcess"`
}

//go:embed templates/*
var content embed.FS

type Handler struct {
	qbit   *qbit.QBit
	logger zerolog.Logger
}

func New(qbit *qbit.QBit) *Handler {
	return &Handler{
		qbit:   qbit,
		logger: logger.New("ui"),
	}
}

var (
	store     = sessions.NewCookieStore([]byte("your-secret-key"))
	templates *template.Template
)

func init() {
	templates = template.Must(template.ParseFS(
		content,
		"templates/layout.html",
		"templates/index.html",
		"templates/download.html",
		"templates/repair.html",
		"templates/config.html",
		"templates/login.html",
		"templates/setup.html",
	))

	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: false,
	}
}

func (ui *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if setup is needed
		cfg := config.Get()
		if cfg.NeedsAuth() && r.URL.Path != "/auth" {
			http.Redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}

		if !cfg.UseAuth {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth check for setup page
		if r.URL.Path == "/auth" {
			next.ServeHTTP(w, r)
			return
		}

		session, _ := store.Get(r, "auth-session")
		auth, ok := session.Values["authenticated"].(bool)

		if !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (ui *Handler) verifyAuth(username, password string) bool {
	// If you're storing hashed password, use bcrypt to compare
	if username == "" {
		return false
	}
	auth := config.Get().GetAuth()
	if auth == nil {
		return false
	}
	if username != auth.Username {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(password))
	return err == nil
}

func (ui *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		data := map[string]interface{}{
			"Page":  "login",
			"Title": "Login",
		}
		_ = templates.ExecuteTemplate(w, "layout", data)
		return
	}

	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if ui.verifyAuth(credentials.Username, credentials.Password) {
		session, _ := store.Get(r, "auth-session")
		session.Values["authenticated"] = true
		session.Values["username"] = credentials.Username
		if err := session.Save(r, w); err != nil {
			http.Error(w, "Error saving session", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Error(w, "Invalid credentials", http.StatusUnauthorized)
}

func (ui *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "auth-session")
	session.Values["authenticated"] = false
	session.Options.MaxAge = -1
	err := session.Save(r, w)
	if err != nil {
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (ui *Handler) SetupHandler(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	authCfg := cfg.GetAuth()

	if !cfg.NeedsSetup() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		data := map[string]interface{}{
			"Page":  "auth",
			"Title": "Auth Setup",
		}
		_ = templates.ExecuteTemplate(w, "layout", data)
		return
	}

	// Handle POST (setup attempt)
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirmPassword")

	if password != confirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Error processing password", http.StatusInternalServerError)
		return
	}

	// Set the credentials
	authCfg.Username = username
	authCfg.Password = string(hashedPassword)

	if err := cfg.SaveAuth(authCfg); err != nil {
		http.Error(w, "Error saving credentials", http.StatusInternalServerError)
		return
	}

	// Create a session
	session, _ := store.Get(r, "auth-session")
	session.Values["authenticated"] = true
	session.Values["username"] = username
	if err := session.Save(r, w); err != nil {
		http.Error(w, "Error saving session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ui *Handler) IndexHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "index",
		"Title": "Torrents",
	}
	_ = templates.ExecuteTemplate(w, "layout", data)
}

func (ui *Handler) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "download",
		"Title": "Download",
	}
	_ = templates.ExecuteTemplate(w, "layout", data)
}

func (ui *Handler) RepairHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "repair",
		"Title": "Repair",
	}
	_ = templates.ExecuteTemplate(w, "layout", data)
}

func (ui *Handler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "config",
		"Title": "Config",
	}
	_ = templates.ExecuteTemplate(w, "layout", data)
}

func (ui *Handler) handleGetArrs(w http.ResponseWriter, r *http.Request) {
	svc := service.GetService()
	request.JSONResponse(w, svc.Arr.GetAll(), http.StatusOK)
}

func (ui *Handler) handleAddContent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	svc := service.GetService()

	results := make([]*qbit.ImportRequest, 0)
	errs := make([]string, 0)

	arrName := r.FormValue("arr")
	notSymlink := r.FormValue("notSymlink") == "true"
	downloadUncached := r.FormValue("downloadUncached") == "true"

	_arr := svc.Arr.Get(arrName)
	if _arr == nil {
		_arr = arr.New(arrName, "", "", false, false, &downloadUncached)
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
			magnet, err := utils.GetMagnetFromUrl(url)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Failed to parse URL %s: %v", url, err))
				continue
			}
			importReq := qbit.NewImportRequest(magnet, _arr, !notSymlink, downloadUncached)
			if err := importReq.Process(ui.qbit); err != nil {
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

			importReq := qbit.NewImportRequest(magnet, _arr, !notSymlink, downloadUncached)
			err = importReq.Process(ui.qbit)
			if err != nil {
				errs = append(errs, fmt.Sprintf("File %s: %v", fileHeader.Filename, err))
				continue
			}
			results = append(results, importReq)
		}
	}

	request.JSONResponse(w, struct {
		Results []*qbit.ImportRequest `json:"results"`
		Errors  []string              `json:"errors,omitempty"`
	}{
		Results: results,
		Errors:  errs,
	}, http.StatusOK)
}

func (ui *Handler) handleRepairMedia(w http.ResponseWriter, r *http.Request) {
	var req RepairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	svc := service.GetService()

	var arrs []string

	if req.ArrName != "" {
		_arr := svc.Arr.Get(req.ArrName)
		if _arr == nil {
			http.Error(w, "No Arrs found to repair", http.StatusNotFound)
			return
		}
		arrs = append(arrs, req.ArrName)
	}

	if req.Async {
		go func() {
			if err := svc.Repair.AddJob(arrs, req.MediaIds, req.AutoProcess, false); err != nil {
				ui.logger.Error().Err(err).Msg("Failed to repair media")
			}
		}()
		request.JSONResponse(w, "Repair process started", http.StatusOK)
		return
	}

	if err := svc.Repair.AddJob([]string{req.ArrName}, req.MediaIds, req.AutoProcess, false); err != nil {
		http.Error(w, fmt.Sprintf("Failed to repair: %v", err), http.StatusInternalServerError)
		return
	}

	request.JSONResponse(w, "Repair completed", http.StatusOK)
}

func (ui *Handler) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	v := version.GetInfo()
	request.JSONResponse(w, v, http.StatusOK)
}

func (ui *Handler) handleGetTorrents(w http.ResponseWriter, r *http.Request) {
	request.JSONResponse(w, ui.qbit.Storage.GetAll("", "", nil), http.StatusOK)
}

func (ui *Handler) handleDeleteTorrent(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	category := r.URL.Query().Get("category")
	removeFromDebrid := r.URL.Query().Get("removeFromDebrid") == "true"
	if hash == "" {
		http.Error(w, "No hash provided", http.StatusBadRequest)
		return
	}
	ui.qbit.Storage.Delete(hash, category, removeFromDebrid)
	w.WriteHeader(http.StatusOK)
}

func (ui *Handler) handleDeleteTorrents(w http.ResponseWriter, r *http.Request) {
	hashesStr := r.URL.Query().Get("hashes")
	removeFromDebrid := r.URL.Query().Get("removeFromDebrid") == "true"
	if hashesStr == "" {
		http.Error(w, "No hashes provided", http.StatusBadRequest)
		return
	}
	hashes := strings.Split(hashesStr, ",")
	ui.qbit.Storage.DeleteMultiple(hashes, removeFromDebrid)
	w.WriteHeader(http.StatusOK)
}

func (ui *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	arrCfgs := make([]config.Arr, 0)
	svc := service.GetService()
	for _, a := range svc.Arr.GetAll() {
		arrCfgs = append(arrCfgs, config.Arr{
			Host:             a.Host,
			Name:             a.Name,
			Token:            a.Token,
			Cleanup:          a.Cleanup,
			SkipRepair:       a.SkipRepair,
			DownloadUncached: a.DownloadUncached,
		})
	}
	cfg.Arrs = arrCfgs
	request.JSONResponse(w, cfg, http.StatusOK)
}

func (ui *Handler) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	// Decode the JSON body
	var updatedConfig config.Config
	if err := json.NewDecoder(r.Body).Decode(&updatedConfig); err != nil {
		ui.logger.Error().Err(err).Msg("Failed to decode config update request")
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get the current configuration
	currentConfig := config.Get()

	// Update fields that can be changed
	currentConfig.LogLevel = updatedConfig.LogLevel
	currentConfig.MinFileSize = updatedConfig.MinFileSize
	currentConfig.MaxFileSize = updatedConfig.MaxFileSize
	currentConfig.AllowedExt = updatedConfig.AllowedExt
	currentConfig.DiscordWebhook = updatedConfig.DiscordWebhook

	// Update QBitTorrent config
	currentConfig.QBitTorrent = updatedConfig.QBitTorrent

	// Update Repair config
	currentConfig.Repair = updatedConfig.Repair

	// Update Debrids
	if len(updatedConfig.Debrids) > 0 {
		currentConfig.Debrids = updatedConfig.Debrids
		// Clear legacy single debrid if using array

	}

	// Update Arrs through the service
	svc := service.GetService()
	svc.Arr.Clear() // Clear existing arrs

	for _, a := range updatedConfig.Arrs {
		svc.Arr.AddOrUpdate(&arr.Arr{
			Name:             a.Name,
			Host:             a.Host,
			Token:            a.Token,
			Cleanup:          a.Cleanup,
			SkipRepair:       a.SkipRepair,
			DownloadUncached: a.DownloadUncached,
		})
	}
	if err := currentConfig.Save(); err != nil {
		http.Error(w, "Error saving config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if restartFunc != nil {
		go func() {
			// Small delay to ensure the response is sent
			time.Sleep(500 * time.Millisecond)
			restartFunc()
		}()
	}

	// Return success
	request.JSONResponse(w, map[string]string{"status": "success"}, http.StatusOK)
}

func (ui *Handler) handleGetRepairJobs(w http.ResponseWriter, r *http.Request) {
	svc := service.GetService()
	request.JSONResponse(w, svc.Repair.GetJobs(), http.StatusOK)
}

func (ui *Handler) handleProcessRepairJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "No job ID provided", http.StatusBadRequest)
		return
	}
	svc := service.GetService()
	if err := svc.Repair.ProcessJob(id); err != nil {
		ui.logger.Error().Err(err).Msg("Failed to process repair job")
	}
	w.WriteHeader(http.StatusOK)
}

func (ui *Handler) handleDeleteRepairJob(w http.ResponseWriter, r *http.Request) {
	// Read ids from body
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		http.Error(w, "No job IDs provided", http.StatusBadRequest)
		return
	}

	svc := service.GetService()
	svc.Repair.DeleteJobs(req.IDs)
	w.WriteHeader(http.StatusOK)
}
