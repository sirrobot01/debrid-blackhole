package webdav

import (
	"bytes"
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/pkg/debrid/debrid"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/version"
	"golang.org/x/net/webdav"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	path "path/filepath"
	"slices"
	"strings"
	"time"
)

type Handler struct {
	Name     string
	logger   zerolog.Logger
	cache    *debrid.Cache
	RootPath string
}

func NewHandler(name string, cache *debrid.Cache, logger zerolog.Logger) *Handler {
	h := &Handler{
		Name:     name,
		cache:    cache,
		logger:   logger,
		RootPath: fmt.Sprintf("/%s", name),
	}
	return h
}

// Mkdir implements webdav.FileSystem
func (h *Handler) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission // Read-only filesystem
}

// RemoveAll implements webdav.FileSystem
func (h *Handler) RemoveAll(ctx context.Context, name string) error {
	name = path.Clean("/" + name)

	rootDir := h.getRootPath()

	if name == rootDir {
		return os.ErrPermission
	}

	torrentName, _ := getName(rootDir, name)
	cachedTorrent := h.cache.GetTorrentByName(torrentName)
	if cachedTorrent == nil {
		h.logger.Debug().Msgf("Torrent not found: %s", torrentName)
		return nil // It's possible that the torrent was removed
	}

	h.cache.OnRemove(cachedTorrent.Id)
	return nil
}

// Rename implements webdav.FileSystem
func (h *Handler) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission // Read-only filesystem
}

func (h *Handler) getRootPath() string {
	return fmt.Sprintf("/webdav/%s", h.Name)
}

func (h *Handler) getTorrentsFolders() []os.FileInfo {
	return h.cache.GetListing()
}

func (h *Handler) getParentItems() []string {
	return []string{"__all__", "torrents", "version.txt"}
}

func (h *Handler) getParentFiles() []os.FileInfo {
	now := time.Now()
	rootFiles := make([]os.FileInfo, 0, len(h.getParentItems()))
	for _, item := range h.getParentItems() {
		f := &FileInfo{
			name:    item,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: now,
			isDir:   true,
		}
		if item == "version.txt" {
			f.isDir = false
			f.size = int64(len(version.GetInfo().String()))
		}
		rootFiles = append(rootFiles, f)
	}
	return rootFiles
}

func (h *Handler) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = path.Clean("/" + name)
	// unescape the path
	name, err := url.PathUnescape(name)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to unescape path")
		return nil, err
	}
	rootDir := h.getRootPath()

	metadataOnly := ctx.Value("metadataOnly") != nil

	now := time.Now()

	// Fast path optimization with a map lookup instead of string comparisons
	switch name {
	case rootDir:
		return &File{
			cache:        h.cache,
			isDir:        true,
			children:     h.getParentFiles(),
			name:         "/",
			metadataOnly: true,
			modTime:      now,
		}, nil
	case path.Join(rootDir, "version.txt"):
		versionInfo := version.GetInfo().String()
		return &File{
			cache:        h.cache,
			isDir:        false,
			content:      []byte(versionInfo),
			name:         "version.txt",
			size:         int64(len(versionInfo)),
			metadataOnly: metadataOnly,
			modTime:      now,
		}, nil
	}

	// Single check for top-level folders
	if h.isParentPath(name) {
		folderName := strings.TrimPrefix(name, rootDir)
		folderName = strings.TrimPrefix(folderName, "/")

		// Only fetch the torrent folders once
		children := h.getTorrentsFolders()

		return &File{
			cache:        h.cache,
			isDir:        true,
			children:     children,
			name:         folderName,
			size:         0,
			metadataOnly: metadataOnly,
			modTime:      now,
		}, nil
	}

	_path := strings.TrimPrefix(name, rootDir)
	parts := strings.Split(strings.TrimPrefix(_path, "/"), "/")

	if len(parts) >= 2 && (slices.Contains(h.getParentItems(), parts[0])) {

		torrentName := parts[1]
		cachedTorrent := h.cache.GetTorrentByName(torrentName)
		if cachedTorrent == nil {
			h.logger.Debug().Msgf("Torrent not found: %s", torrentName)
			return nil, os.ErrNotExist
		}

		if len(parts) == 2 {
			// Torrent folder level
			return &File{
				cache:        h.cache,
				torrentId:    cachedTorrent.Id,
				isDir:        true,
				children:     h.getFileInfos(cachedTorrent.Torrent),
				name:         cachedTorrent.Name,
				size:         cachedTorrent.Size,
				metadataOnly: metadataOnly,
				modTime:      cachedTorrent.AddedOn,
			}, nil
		}

		// Torrent file level
		filename := strings.Join(parts[2:], "/")
		if file, ok := cachedTorrent.Files[filename]; ok {
			fi := &File{
				cache:        h.cache,
				torrentId:    cachedTorrent.Id,
				fileId:       file.Id,
				isDir:        false,
				name:         file.Name,
				size:         file.Size,
				link:         file.Link,
				metadataOnly: metadataOnly,
				modTime:      cachedTorrent.AddedOn,
			}
			return fi, nil
		}
	}

	h.logger.Info().Msgf("File not found: %s", name)
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

func (h *Handler) getFileInfos(torrent *types.Torrent) []os.FileInfo {
	files := make([]os.FileInfo, 0, len(torrent.Files))
	now := time.Now()
	for _, file := range torrent.Files {
		files = append(files, &FileInfo{
			name:    file.Name,
			size:    file.Size,
			mode:    0644,
			modTime: now,
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

	// Cache PROPFIND responses for a short time to reduce load.
	if r.Method == "PROPFIND" {
		// Determine the Depth; default to "1" if not provided.
		// Set metadata only
		ctx := context.WithValue(r.Context(), "metadataOnly", true)
		r = r.WithContext(ctx)
		cleanPath := path.Clean(r.URL.Path)
		if r.Header.Get("Depth") == "" {
			r.Header.Set("Depth", "1")
		}

		// Reject "infinity" depth
		if r.Header.Get("Depth") == "infinity" {
			r.Header.Set("Depth", "1")
		}
		depth := r.Header.Get("Depth")
		// Use both path and Depth header to form the cache key.
		cacheKey := fmt.Sprintf("propfind:%s:%s", cleanPath, depth)

		// Determine TTL based on the requested folder:
		// - If the path is exactly the parent folder (which changes frequently),
		//   use a short TTL.
		// - Otherwise, for deeper (torrent folder) paths, use a longer TTL.
		ttl := 1 * time.Minute
		if h.isParentPath(r.URL.Path) {
			ttl = 30 * time.Second
		}

		if served := h.serveFromCacheIfValid(w, r, cacheKey, ttl); served {
			return
		}

		// No valid cache entry; process the PROPFIND request.
		responseRecorder := httptest.NewRecorder()
		handler := &webdav.Handler{
			FileSystem: h,
			LockSystem: webdav.NewMemLS(),
			Logger: func(r *http.Request, err error) {
				if err != nil {
					h.logger.Error().Err(err).Msg("WebDAV error")
				}
			},
		}
		handler.ServeHTTP(responseRecorder, r)
		responseData := responseRecorder.Body.Bytes()
		gzippedData := request.Gzip(responseData)

		// Create compressed version

		h.cache.PropfindResp.Store(cacheKey, debrid.PropfindResponse{
			Data:        responseData,
			GzippedData: gzippedData,
			Ts:          time.Now(),
		})

		// Forward the captured response to the client.
		for k, v := range responseRecorder.Header() {
			w.Header()[k] = v
		}

		if acceptsGzip(r) {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(gzippedData)))
			w.WriteHeader(responseRecorder.Code)
			_, _ = w.Write(gzippedData)
		} else {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseData)))
			w.WriteHeader(responseRecorder.Code)
			_, _ = w.Write(responseData)
		}
		return
	}

	// Handle GET requests for file/directory content
	if r.Method == "GET" {
		f, err := h.OpenFile(r.Context(), r.URL.Path, os.O_RDONLY, 0)
		if err != nil {
			h.logger.Error().Err(err).Str("path", r.URL.Path).Msg("Failed to open file")
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		fi, err := f.Stat()
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to stat file")
			http.Error(w, "Server Error", http.StatusInternalServerError)
			return
		}

		// If the target is a directory, use your directory listing logic.
		if fi.IsDir() {
			h.serveDirectory(w, r, f)
			return
		}

		rs, ok := f.(io.ReadSeeker)
		if !ok {
			// If not, read the entire file into memory as a fallback.
			buf, err := io.ReadAll(f)
			if err != nil {
				h.logger.Error().Err(err).Msg("Failed to read file content")
				http.Error(w, "Server Error", http.StatusInternalServerError)
				return
			}
			rs = bytes.NewReader(buf)
		}
		fileName := fi.Name()
		contentType := getContentType(fileName)
		w.Header().Set("Content-Type", contentType)

		// Serve the file with the correct modification time.
		// http.ServeContent automatically handles Range requests.
		http.ServeContent(w, r, fileName, fi.ModTime(), rs)
		return
	}

	// Fallback: for other methods, use the standard WebDAV handler.
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
	handler.ServeHTTP(w, r)
}

func getContentType(fileName string) string {
	contentType := "application/octet-stream"

	// Determine content type based on file extension
	switch {
	case strings.HasSuffix(fileName, ".mp4"):
		contentType = "video/mp4"
	case strings.HasSuffix(fileName, ".mkv"):
		contentType = "video/x-matroska"
	case strings.HasSuffix(fileName, ".avi"):
		contentType = "video/x-msvideo"
	case strings.HasSuffix(fileName, ".mov"):
		contentType = "video/quicktime"
	case strings.HasSuffix(fileName, ".m4v"):
		contentType = "video/x-m4v"
	case strings.HasSuffix(fileName, ".ts"):
		contentType = "video/mp2t"
	case strings.HasSuffix(fileName, ".srt"):
		contentType = "application/x-subrip"
	case strings.HasSuffix(fileName, ".vtt"):
		contentType = "text/vtt"
	}
	return contentType
}

func (h *Handler) isParentPath(_path string) bool {
	rootPath := h.getRootPath()
	parents := h.getParentItems()
	for _, p := range parents {
		if path.Clean(_path) == path.Clean(path.Join(rootPath, p)) {
			return true
		}
	}
	return false
}

func (h *Handler) serveFromCacheIfValid(w http.ResponseWriter, r *http.Request, cacheKey string, ttl time.Duration) bool {
	respCache, ok := h.cache.PropfindResp.Load(cacheKey)
	if !ok {
		return false
	}

	if time.Since(respCache.Ts) >= ttl {
		// Remove expired cache entry
		return false
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	if acceptsGzip(r) && len(respCache.GzippedData) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(respCache.GzippedData)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respCache.GzippedData)
	} else {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(respCache.Data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respCache.Data)
	}
	return true
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
	funcMap := template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"formatSize": func(bytes int64) string {
			const (
				KB = 1024
				MB = 1024 * KB
				GB = 1024 * MB
				TB = 1024 * GB
			)

			var size float64
			var unit string

			switch {
			case bytes >= TB:
				size = float64(bytes) / TB
				unit = "TB"
			case bytes >= GB:
				size = float64(bytes) / GB
				unit = "GB"
			case bytes >= MB:
				size = float64(bytes) / MB
				unit = "MB"
			case bytes >= KB:
				size = float64(bytes) / KB
				unit = "KB"
			default:
				size = float64(bytes)
				unit = "bytes"
			}

			// Format to 2 decimal places for larger units, no decimals for bytes
			if unit == "bytes" {
				return fmt.Sprintf("%.0f %s", size, unit)
			}
			return fmt.Sprintf("%.2f %s", size, unit)
		},
	}
	tmpl, err := template.New("directory").Funcs(funcMap).Parse(directoryTemplate)
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
