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
	"slices"
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
		h.cache.GetClient().DeleteTorrent(cachedTorrent.Torrent)
		go h.cache.refreshListings()
		go h.cache.refreshTorrents()
		go h.cache.resetPropfindResponse()
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
	if h.isParentPath(name) {
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

	// Cache PROPFIND responses for a short time to reduce load.
	if r.Method == "PROPFIND" {
		// Determine the Depth; default to "1" if not provided.
		depth := r.Header.Get("Depth")
		if depth == "" {
			depth = "1"
		}
		// Use both path and Depth header to form the cache key.
		cacheKey := fmt.Sprintf("propfind:%s:%s", r.URL.Path, depth)

		// Determine TTL based on the requested folder:
		// - If the path is exactly the parent folder (which changes frequently),
		//   use a short TTL.
		// - Otherwise, for deeper (torrent folder) paths, use a longer TTL.
		var ttl time.Duration
		if h.isParentPath(r.URL.Path) {
			ttl = 10 * time.Second
		} else {
			ttl = 1 * time.Minute
		}

		// Check if we have a cached response that hasn't expired.
		if cached, ok := h.cache.propfindResp.Load(cacheKey); ok {
			if respCache, ok := cached.(propfindResponse); ok {
				if time.Since(respCache.ts) < ttl {
					w.Header().Set("Content-Type", "application/xml; charset=utf-8")
					w.Header().Set("Content-Length", fmt.Sprintf("%d", len(respCache.data)))
					w.Write(respCache.data)
					return
				}
			}
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

		// Store the new response in the cache.
		h.cache.propfindResp.Store(cacheKey, propfindResponse{
			data: responseData,
			ts:   time.Now(),
		})

		// Forward the captured response to the client.
		for k, v := range responseRecorder.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(responseRecorder.Code)
		w.Write(responseData)
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

		// Set headers to indicate support for range requests and content type.
		//fileName := fi.Name()
		//w.Header().Set("Accept-Ranges", "bytes")
		//w.Header().Set("Content-Type", getContentType(fileName))
		//
		//// If a Range header is provided, parse and handle partial content.
		//rangeHeader := r.Header.Get("Range")
		//if rangeHeader != "" {
		//	parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
		//	if len(parts) == 2 {
		//		start, startErr := strconv.ParseInt(parts[0], 10, 64)
		//		end := fi.Size() - 1
		//		if parts[1] != "" {
		//			var endErr error
		//			end, endErr = strconv.ParseInt(parts[1], 10, 64)
		//			if endErr != nil {
		//				end = fi.Size() - 1
		//			}
		//		}
		//
		//		if startErr == nil && start < fi.Size() {
		//			if start > end {
		//				start, end = end, start
		//			}
		//			if end >= fi.Size() {
		//				end = fi.Size() - 1
		//			}
		//
		//			contentLength := end - start + 1
		//			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fi.Size()))
		//			w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
		//			w.WriteHeader(http.StatusPartialContent)
		//
		//			// Attempt to cast to your concrete File type to call Seek.
		//			if file, ok := f.(*File); ok {
		//				_, err = file.Seek(start, io.SeekStart)
		//				if err != nil {
		//					h.logger.Error().Err(err).Msg("Failed to seek in file")
		//					http.Error(w, "Server Error", http.StatusInternalServerError)
		//					return
		//				}
		//
		//				limitedReader := io.LimitReader(f, contentLength)
		//				h.ioCopy(limitedReader, w)
		//				return
		//			}
		//		}
		//	}
		//}
		//w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
		//h.ioCopy(f, w)
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
		if _path == path.Join(rootPath, p) {
			return true
		}
	}
	return false
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
