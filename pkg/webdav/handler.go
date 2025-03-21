package webdav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"golang.org/x/net/webdav"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	path "path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	Name         string
	logger       zerolog.Logger
	cache        *debrid.Cache
	lastRefresh  time.Time
	refreshMutex sync.Mutex
	RootPath     string
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

	torrentName, filename := getName(rootDir, name)
	cachedTorrent := h.cache.GetTorrentByName(torrentName)
	if cachedTorrent == nil {
		return os.ErrNotExist
	}

	if filename == "" {
		h.cache.GetClient().DeleteTorrent(cachedTorrent.Torrent.Id)
		h.cache.OnRemove(cachedTorrent.Id)
		return nil
	}

	return os.ErrPermission
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
			f.size = int64(len("v1.0.0"))
		}
		rootFiles = append(rootFiles, f)
	}
	return rootFiles
}

func (h *Handler) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = path.Clean("/" + name)
	rootDir := h.getRootPath()

	metadataOnly := false
	if ctx.Value("metadataOnly") != nil {
		metadataOnly = true
	}

	// Fast path optimization with a map lookup instead of string comparisons
	switch name {
	case rootDir:
		return &File{
			cache:        h.cache,
			isDir:        true,
			children:     h.getParentFiles(),
			name:         "/",
			metadataOnly: metadataOnly,
		}, nil
	case path.Join(rootDir, "version.txt"):
		return &File{
			cache:        h.cache,
			isDir:        false,
			content:      []byte("v1.0.0"),
			name:         "version.txt",
			size:         int64(len("v1.0.0")),
			metadataOnly: metadataOnly,
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
				downloadLink: file.DownloadLink,
				metadataOnly: metadataOnly,
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
		depth := r.Header.Get("Depth")
		if depth == "" {
			depth = "1"
		}
		// Use both path and Depth header to form the cache key.
		cacheKey := fmt.Sprintf("propfind:%s:%s", cleanPath, depth)

		// Determine TTL based on the requested folder:
		// - If the path is exactly the parent folder (which changes frequently),
		//   use a short TTL.
		// - Otherwise, for deeper (torrent folder) paths, use a longer TTL.
		ttl := 30 * time.Minute
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
			w.Write(gzippedData)
		} else {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseData)))
			w.WriteHeader(responseRecorder.Code)
			w.Write(responseData)
		}
		return
	}

	// Handle GET requests for file/directory content
	if r.Method == "GET" {
		f, err := h.OpenFile(r.Context(), r.URL.Path, os.O_RDONLY, 0)
		if err != nil {
			h.logger.Debug().Err(err).Str("path", r.URL.Path).Msg("Failed to open file")
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
		w.Write(respCache.GzippedData)
	} else {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(respCache.Data)))
		w.WriteHeader(http.StatusOK)
		w.Write(respCache.Data)
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

func (h *Handler) ioCopy(reader io.Reader, w io.Writer) (int64, error) {
	// Start with a smaller buffer for faster first byte delivery.
	buf := make([]byte, 4*1024) // 8KB initial buffer
	totalWritten := int64(0)
	firstChunk := true

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			nw, ew := w.Write(buf[:n])
			if ew != nil {
				var opErr *net.OpError
				if errors.As(ew, &opErr) && opErr.Err.Error() == "write: broken pipe" {
					h.logger.Debug().Msg("Client closed connection (normal for streaming)")
					return totalWritten, ew
				}
				return totalWritten, ew
			}
			totalWritten += int64(nw)

			// Flush immediately after the first chunk.
			if firstChunk {
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				firstChunk = false
				// Increase buffer size for subsequent reads.
				buf = make([]byte, 512*1024) // 64KB buffer after first chunk
			} else if totalWritten%(2*1024*1024) < int64(n) {
				// Flush roughly every 2MB of data transferred.
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				h.logger.Error().Err(err).Msg("Error reading from file")
			}
			break
		}
	}

	return totalWritten, nil
}
