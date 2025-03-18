package webdav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"golang.org/x/net/webdav"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	Name          string
	logger        zerolog.Logger
	cache         *Cache
	lastRefresh   time.Time
	refreshMutex  sync.Mutex
	RootPath      string
	responseCache sync.Map
	cacheTTL      time.Duration
	ctx           context.Context
}

func NewHandler(name string, cache *Cache, logger zerolog.Logger) *Handler {
	h := &Handler{
		Name:     name,
		cache:    cache,
		logger:   logger,
		RootPath: fmt.Sprintf("/%s", name),
		ctx:      context.Background(),
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

	rootDir := h.getParentRootPath()

	if name == rootDir {
		return os.ErrPermission
	}

	torrentName, filename := getName(rootDir, name)
	cachedTorrent := h.cache.GetTorrentByName(torrentName)
	if cachedTorrent == nil {
		return os.ErrNotExist
	}

	if filename == "" {
		h.cache.GetClient().DeleteTorrent(cachedTorrent.Torrent)
		go h.cache.refreshListings()
		return nil
	}

	return os.ErrPermission
}

// Rename implements webdav.FileSystem
func (h *Handler) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission // Read-only filesystem
}

func (h *Handler) getParentRootPath() string {
	return fmt.Sprintf("/webdav/%s", h.Name)
}

func (h *Handler) getTorrentsFolders() []os.FileInfo {
	return h.cache.GetListing()
}

func (h *Handler) getParentFiles() []os.FileInfo {
	now := time.Now()
	rootFiles := []os.FileInfo{
		&FileInfo{
			name:    "__all__",
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: now,
			isDir:   true,
		},
		&FileInfo{
			name:    "torrents",
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: now,
			isDir:   true,
		},
		&FileInfo{
			name:    "version.txt",
			size:    int64(len("v1.0.0")),
			mode:    0644,
			modTime: now,
			isDir:   false,
		},
	}
	return rootFiles
}

func (h *Handler) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = path.Clean("/" + name)
	rootDir := h.getParentRootPath()

	// Fast path optimization with a map lookup instead of string comparisons
	switch name {
	case rootDir:
		return &File{
			cache:    h.cache,
			isDir:    true,
			children: h.getParentFiles(),
			name:     "/",
		}, nil
	case path.Join(rootDir, "version.txt"):
		return &File{
			cache:   h.cache,
			isDir:   false,
			content: []byte("v1.0.0"),
			name:    "version.txt",
			size:    int64(len("v1.0.0")),
		}, nil
	}

	// Single check for top-level folders
	if name == path.Join(rootDir, "__all__") || name == path.Join(rootDir, "torrents") {
		folderName := strings.TrimPrefix(name, rootDir)
		folderName = strings.TrimPrefix(folderName, "/")

		// Only fetch the torrent folders once
		children := h.getTorrentsFolders()

		return &File{
			cache:    h.cache,
			isDir:    true,
			children: children,
			name:     folderName,
			size:     0,
		}, nil
	}

	_path := strings.TrimPrefix(name, rootDir)
	parts := strings.Split(strings.TrimPrefix(_path, "/"), "/")

	if len(parts) >= 2 && (parts[0] == "__all__" || parts[0] == "torrents") {

		torrentName := parts[1]
		cachedTorrent := h.cache.GetTorrentByName(torrentName)
		if cachedTorrent == nil {
			h.logger.Debug().Msgf("Torrent not found: %s", torrentName)
			return nil, os.ErrNotExist
		}

		if len(parts) == 2 {
			// Torrent folder level
			return &File{
				cache:     h.cache,
				torrentId: cachedTorrent.Id,
				isDir:     true,
				children:  h.getFileInfos(cachedTorrent.Torrent),
				name:      cachedTorrent.Name,
				size:      cachedTorrent.Size,
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

func (h *Handler) getFileInfos(torrent *torrent.Torrent) []os.FileInfo {
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

	//Add specific PROPFIND optimization
	if r.Method == "PROPFIND" {
		propfindStart := time.Now()

		// Check if this is the slow path we identified
		if strings.Contains(r.URL.Path, "__all__") {
			// Fast path for this specific directory
			depth := r.Header.Get("Depth")
			if depth == "1" || depth == "" {
				// This is a listing request

				// Use a cached response if available
				cachedKey := "propfind_" + r.URL.Path
				if cachedResponse, ok := h.responseCache.Load(cachedKey); ok {
					responseData := cachedResponse.([]byte)
					w.Header().Set("Content-Type", "application/xml; charset=utf-8")
					w.Header().Set("Content-Length", fmt.Sprintf("%d", len(responseData)))
					w.Write(responseData)
					return
				}

				// Otherwise process normally but cache the result
				responseRecorder := httptest.NewRecorder()

				// Process the request with the standard handler
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

				// Cache the response for future requests
				responseData := responseRecorder.Body.Bytes()
				h.responseCache.Store(cachedKey, responseData)

				// Send to the real client
				for k, v := range responseRecorder.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(responseRecorder.Code)
				w.Write(responseData)
				return
			}
		}

		h.logger.Debug().
			Dur("propfind_prepare", time.Since(propfindStart)).
			Msg("Proceeding with standard PROPFIND")
	}

	// Check if this is a GET request for a file
	if r.Method == "GET" {
		openStart := time.Now()
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

		if fi.IsDir() {
			dirStart := time.Now()
			h.serveDirectory(w, r, f)
			h.logger.Info().
				Dur("directory_time", time.Since(dirStart)).
				Msg("Directory served")
			return
		}

		// For file requests, use http.ServeContent.
		// Ensure f implements io.ReadSeeker.
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

		// Set Content-Type based on file name.
		fileName := fi.Name()
		contentType := getContentType(fileName)
		w.Header().Set("Content-Type", contentType)

		// Serve the file with the correct modification time.
		// http.ServeContent automatically handles Range requests.
		http.ServeContent(w, r, fileName, fi.ModTime(), rs)
		h.logger.Info().
			Dur("open_attempt_time", time.Since(openStart)).
			Msg("Served file using ServeContent")
		return
	}

	// Default to standard WebDAV handler for other requests
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
	// Start with a smaller initial buffer for faster first byte time
	buffer := make([]byte, 8*1024) // 8KB initial buffer
	written := int64(0)

	// First chunk needs to be delivered ASAP
	firstChunk := true

	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			nw, ew := w.Write(buffer[:n])
			if ew != nil {
				var opErr *net.OpError
				if errors.As(ew, &opErr) && opErr.Err.Error() == "write: broken pipe" {
					h.logger.Debug().Msg("Client closed connection (normal for streaming)")
				}
				break
			}
			written += int64(nw)

			// Flush immediately after first chunk, then less frequently
			if firstChunk {
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				firstChunk = false

				// Increase buffer size after first chunk
				buffer = make([]byte, 64*1024) // 512KB for subsequent reads
			} else if written%(2*1024*1024) < int64(n) { // Flush every 2MB
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

	return written, nil
}
