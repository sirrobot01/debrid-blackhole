package webdav

import (
	"bufio"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
	"io"
	"net/http"
	"os"
	"time"
)

var sharedClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:       100,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		DisableKeepAlives:  false,
		Proxy:              http.ProxyFromEnvironment,
	},
	Timeout: 0,
}

type File struct {
	cache     *debrid.Cache
	fileId    string
	torrentId string

	size         int64
	offset       int64
	isDir        bool
	children     []os.FileInfo
	reader       io.ReadCloser
	seekPending  bool
	content      []byte
	name         string
	metadataOnly bool

	downloadLink string
	link         string
}

type bufferedReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

// Create a new bufferedReadCloser with a larger buffer
func newBufferedReadCloser(rc io.ReadCloser) *bufferedReadCloser {
	return &bufferedReadCloser{
		Reader: bufio.NewReaderSize(rc, 64*1024), // Increase to 1MB buffer
		closer: rc,
	}
}

// Close implements ReadCloser interface
func (brc *bufferedReadCloser) Close() error {
	return brc.closer.Close()
}

// File interface implementations for File

func (f *File) Close() error {
	if f.reader != nil {
		f.reader.Close()
		f.reader = nil
	}
	return nil
}

func (f *File) GetDownloadLink() string {
	// Check if we already have a final URL cached

	if f.downloadLink != "" && isValidURL(f.downloadLink) {
		return f.downloadLink
	}
	downloadLink := f.cache.GetDownloadLink(f.torrentId, f.name, f.link)
	if downloadLink != "" && isValidURL(downloadLink) {
		f.downloadLink = downloadLink
		return downloadLink
	}

	return ""
}

func (f *File) Read(p []byte) (n int, err error) {
	if f.isDir {
		return 0, os.ErrInvalid
	}
	if f.metadataOnly {
		return 0, io.EOF
	}

	// If file content is preloaded, read from memory.
	if f.content != nil {
		if f.offset >= int64(len(f.content)) {
			return 0, io.EOF
		}
		n = copy(p, f.content[f.offset:])
		f.offset += int64(n)
		return n, nil
	}

	// If we haven't started streaming the file yet or need to reposition
	if f.reader == nil || f.seekPending {
		// Close existing reader if we're repositioning
		if f.reader != nil && f.seekPending {
			f.reader.Close()
			f.reader = nil
		}

		downloadLink := f.GetDownloadLink()
		if downloadLink == "" {
			return 0, fmt.Errorf("failed to get download link for file")
		}

		// Create an HTTP GET request to the file's URL.
		req, err := http.NewRequest("GET", downloadLink, nil)
		if err != nil {
			return 0, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		// Request only the bytes starting from our current offset
		if f.offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", f.offset))
		}

		// Add important headers for streaming
		req.Header.Set("Connection", "keep-alive")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("User-Agent", "Infuse/7.0.2 (iOS)")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")

		resp, err := sharedClient.Do(req)
		if err != nil {
			return 0, fmt.Errorf("HTTP request error: %w", err)
		}

		// Check response codes more carefully
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return 0, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
		}

		f.reader = newBufferedReadCloser(resp.Body)
		f.seekPending = false
	}

	// Read data from the HTTP stream.
	n, err = f.reader.Read(p)
	f.offset += int64(n)

	if err == io.EOF {
		f.reader.Close()
		f.reader = nil
	} else if err != nil {
		f.reader.Close()
		f.reader = nil
	}

	return n, err
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	if f.isDir {
		return 0, os.ErrInvalid
	}

	newOffset := f.offset
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset += offset
	case io.SeekEnd:
		newOffset = f.size + offset
	default:
		return 0, os.ErrInvalid
	}

	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > f.size {
		newOffset = f.size
	}

	// Only mark seek as pending if position actually changed
	if newOffset != f.offset {
		f.offset = newOffset
		f.seekPending = true
	}
	return f.offset, nil
}

func (f *File) Stat() (os.FileInfo, error) {
	if f.isDir {
		return &FileInfo{
			name:    f.name,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	return &FileInfo{
		name:    f.name,
		size:    f.size,
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}, nil
}

func (f *File) ReadAt(p []byte, off int64) (n int, err error) {
	// Save current position

	// Seek to requested position
	_, err = f.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}

	// Read the data
	n, err = f.Read(p)

	// Don't restore position for Infuse compatibility
	// Infuse expects sequential reads after the initial seek

	return n, err
}

func (f *File) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

func (f *File) Readdir(count int) ([]os.FileInfo, error) {
	if !f.isDir {
		return nil, os.ErrInvalid
	}

	if count <= 0 {
		return f.children, nil
	}

	if len(f.children) == 0 {
		return nil, io.EOF
	}

	if count > len(f.children) {
		count = len(f.children)
	}

	files := f.children[:count]
	f.children = f.children[count:]
	return files, nil
}
