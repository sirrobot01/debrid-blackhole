package webdav

import (
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/cache"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"golang.org/x/net/webdav"
	"html/template"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Handler struct {
	Name         string
	logger       zerolog.Logger
	cache        *cache.Cache
	rootListing  atomic.Value
	lastRefresh  time.Time
	refreshMutex sync.Mutex
	RootPath     string
}

func NewHandler(name string, cache *cache.Cache, logger zerolog.Logger) *Handler {
	h := &Handler{
		Name:     name,
		cache:    cache,
		logger:   logger,
		RootPath: fmt.Sprintf("/%s", name),
	}

	h.refreshRootListing()

	// Start background refresh
	go h.backgroundRefresh()

	return h
}

func (h *Handler) backgroundRefresh() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.refreshRootListing()
	}
}

func (h *Handler) refreshRootListing() {
	h.refreshMutex.Lock()
	defer h.refreshMutex.Unlock()

	if time.Since(h.lastRefresh) < time.Minute {
		return
	}

	torrents := h.cache.GetTorrentNames()
	files := make([]os.FileInfo, 0, len(torrents))

	for name, cachedTorrent := range torrents {
		if cachedTorrent != nil && cachedTorrent.Torrent != nil {
			files = append(files, &FileInfo{
				name:    name,
				size:    0,
				mode:    0755 | os.ModeDir,
				modTime: time.Now(),
				isDir:   true,
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	h.rootListing.Store(files)
	h.lastRefresh = time.Now()
}

func (h *Handler) getParentRootPath() string {
	return fmt.Sprintf("/webdav/%s", h.Name)
}

func (h *Handler) getRootFileInfos() []os.FileInfo {
	if listing := h.rootListing.Load(); listing != nil {
		return listing.([]os.FileInfo)
	}
	return []os.FileInfo{}
}

// Mkdir implements webdav.FileSystem
func (h *Handler) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission // Read-only filesystem
}

// RemoveAll implements webdav.FileSystem
func (h *Handler) RemoveAll(ctx context.Context, name string) error {
	return os.ErrPermission // Read-only filesystem
}

// Rename implements webdav.FileSystem
func (h *Handler) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission // Read-only filesystem
}

func (h *Handler) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = path.Clean("/" + name)

	// Fast path for root directory
	if name == h.getParentRootPath() {
		return &File{
			cache:    h.cache,
			isDir:    true,
			children: h.getRootFileInfos(),
		}, nil
	}

	// Remove root directory from path
	name = strings.TrimPrefix(name, h.getParentRootPath())
	name = strings.TrimPrefix(name, "/")
	parts := strings.SplitN(name, "/", 2)

	// Get torrent from cache using sync.Map
	cachedTorrent := h.cache.GetTorrentByName(parts[0])
	if cachedTorrent == nil {
		h.logger.Debug().Msgf("Torrent not found: %s", parts[0])
		return nil, os.ErrNotExist
	}

	if len(parts) == 1 {
		return &File{
			cache:         h.cache,
			cachedTorrent: cachedTorrent,
			isDir:         true,
			children:      h.getTorrentFileInfos(cachedTorrent.Torrent),
		}, nil
	}

	// Use a map for faster file lookup
	fileMap := make(map[string]*torrent.File, len(cachedTorrent.Torrent.Files))
	for i := range cachedTorrent.Torrent.Files {
		fileMap[cachedTorrent.Torrent.Files[i].Name] = &cachedTorrent.Torrent.Files[i]
	}

	if file, ok := fileMap[parts[1]]; ok {
		return &File{
			cache:         h.cache,
			cachedTorrent: cachedTorrent,
			file:          file,
			isDir:         false,
		}, nil
	}

	h.logger.Debug().Msgf("File not found: %s", name)
	return nil, os.ErrNotExist
}

// Stat implements webdav.FileSystem
func (h *Handler) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	f, err := h.OpenFile(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return f.Stat()
}

func (h *Handler) getTorrentFileInfos(torrent *torrent.Torrent) []os.FileInfo {
	files := make([]os.FileInfo, 0, len(torrent.Files))
	for _, file := range torrent.Files {
		files = append(files, &FileInfo{
			name:    file.Name,
			size:    file.Size,
			mode:    0644,
			modTime: time.Now(),
			isDir:   false,
		})
	}
	return files
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle OPTIONS
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Create WebDAV handler
	handler := &webdav.Handler{
		FileSystem: h,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				h.logger.Error().
					Err(err).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Msg("WebDAV error")
			}
		},
	}

	// Special handling for GET requests on directories
	if r.Method == "GET" {
		if f, err := h.OpenFile(r.Context(), r.URL.Path, os.O_RDONLY, 0); err == nil {
			if fi, err := f.Stat(); err == nil && fi.IsDir() {
				h.serveDirectory(w, r, f)
				return
			}
			f.Close()
		}
	}
	handler.ServeHTTP(w, r)
}

func (h *Handler) serveDirectory(w http.ResponseWriter, r *http.Request, file webdav.File) {
	var children []os.FileInfo
	if f, ok := file.(*File); ok {
		children = f.children
	} else {
		var err error
		children, err = file.Readdir(-1)
		if err != nil {
			http.Error(w, "Failed to list directory", http.StatusInternalServerError)
			return
		}
	}

	// Clean and prepare the path
	cleanPath := path.Clean(r.URL.Path)
	parentPath := path.Dir(cleanPath)
	showParent := cleanPath != "/" && parentPath != "." && parentPath != cleanPath

	// Prepare template data
	data := struct {
		Path       string
		ParentPath string
		ShowParent bool
		Children   []os.FileInfo
	}{
		Path:       cleanPath,
		ParentPath: parentPath,
		ShowParent: showParent,
		Children:   children,
	}

	// Parse and execute template
	tmpl, err := template.New("directory").Parse(directoryTemplate)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to parse directory template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		h.logger.Error().Err(err).Msg("Failed to execute directory template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}
