package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/gorilla/sessions"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit"
	"github.com/sirrobot01/debrid-blackhole/pkg/service"
	"golang.org/x/crypto/bcrypt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
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

//go:embed web/*
var content embed.FS

type Handler struct {
	qbit   *qbit.QBit
	logger zerolog.Logger
}

func New(qbit *qbit.QBit) *Handler {
	cfg := config.GetConfig()
	return &Handler{
		qbit:   qbit,
		logger: logger.NewLogger("ui", cfg.LogLevel, os.Stdout),
	}
}

var (
	store     = sessions.NewCookieStore([]byte("your-secret-key")) // Change this to a secure key
	templates *template.Template
)

func init() {
	templates = template.Must(template.ParseFS(
		content,
		"web/layout.html",
		"web/index.html",
		"web/download.html",
		"web/repair.html",
		"web/config.html",
		"web/login.html",
		"web/setup.html",
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
		cfg := config.GetConfig()
		if cfg.NeedsSetup() && r.URL.Path != "/setup" {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		if !cfg.UseAuth {
			next.ServeHTTP(w, r)
			return
		}

		// Skip auth check for setup page
		if r.URL.Path == "/setup" {
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
	auth := config.GetConfig().GetAuth()
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
		if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
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
	cfg := config.GetConfig()
	authCfg := cfg.GetAuth()

	if !cfg.NeedsSetup() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == "GET" {
		data := map[string]interface{}{
			"Page":  "setup",
			"Title": "Setup",
		}
		if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
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
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *Handler) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "download",
		"Title": "Download",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *Handler) RepairHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "repair",
		"Title": "Repair",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (ui *Handler) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Page":  "config",
		"Title": "Config",
	}
	if err := templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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

	_arr := svc.Arr.Get(arrName)
	if _arr == nil {
		_arr = arr.New(arrName, "", "", false)
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
			importReq := qbit.NewImportRequest(url, _arr, !notSymlink)
			err := importReq.Process(ui.qbit)
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

			importReq := qbit.NewImportRequest(magnet.Link, _arr, !notSymlink)
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

	_arr := svc.Arr.Get(req.ArrName)
	if _arr == nil {
		http.Error(w, "No Arrs found to repair", http.StatusNotFound)
		return
	}

	if req.Async {
		go func() {
			if err := svc.Repair.Repair([]*arr.Arr{_arr}, req.MediaIds); err != nil {
				ui.logger.Error().Err(err).Msg("Failed to repair media")
			}
		}()
		request.JSONResponse(w, "Repair process started", http.StatusOK)
		return
	}

	if err := svc.Repair.Repair([]*arr.Arr{_arr}, req.MediaIds); err != nil {
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
	if hash == "" {
		http.Error(w, "No hash provided", http.StatusBadRequest)
		return
	}
	ui.qbit.Storage.Delete(hash)
	w.WriteHeader(http.StatusOK)
}

func (ui *Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.GetConfig()
	arrCfgs := make([]config.Arr, 0)
	svc := service.GetService()
	for _, a := range svc.Arr.GetAll() {
		arrCfgs = append(arrCfgs, config.Arr{Host: a.Host, Name: a.Name, Token: a.Token})
	}
	cfg.Arrs = arrCfgs
	request.JSONResponse(w, cfg, http.StatusOK)
}
