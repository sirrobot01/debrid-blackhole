package webdav

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var sharedClient = &http.Client{
	Transport: &http.Transport{
		// These settings help maintain persistent connections.
		MaxIdleConns:       100,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
		DisableKeepAlives:  false,
	},
	Timeout: 0,
}

type File struct {
	cache     *Cache
	fileId    string
	torrentId string

	size        int64
	offset      int64
	isDir       bool
	children    []os.FileInfo
	reader      io.ReadCloser
	seekPending bool
	content     []byte
	name        string

	downloadLink string
	link         string
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
	if f.downloadLink != "" {
		return f.downloadLink
	}
	downloadLink := f.cache.GetDownloadLink(f.torrentId, f.name, f.link)
	if downloadLink != "" {
		f.downloadLink = downloadLink
		return downloadLink
	}

	return ""
}

func (f *File) Read(p []byte) (n int, err error) {
	if f.isDir {
		return 0, os.ErrInvalid
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

	// If we haven't started streaming or a seek was requested,
	// close the existing stream and start a new HTTP GET request.
	if f.reader == nil || f.seekPending {
		if f.reader != nil && f.seekPending {
			f.reader.Close()
			f.reader = nil
		}

		// Create a new HTTP GET request for the file's URL.
		req, err := http.NewRequest("GET", f.GetDownloadLink(), nil)
		if err != nil {
			return 0, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		// If we've already read some data, request only the remaining bytes.
		if f.offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", f.offset))
		}

		// Execute the HTTP request.
		resp, err := sharedClient.Do(req)
		if err != nil {
			return 0, fmt.Errorf("HTTP request error: %w", err)
		}

		// Accept a 200 (OK) or 206 (Partial Content) status.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return 0, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
		}

		// Store the response body as our reader.
		f.reader = resp.Body
		// Reset the seek pending flag now that we've reinitialized the reader.
		f.seekPending = false
	}

	// Read data from the HTTP stream.
	n, err = f.reader.Read(p)
	f.offset += int64(n)

	// When we reach the end of the stream, close the reader.
	if err == io.EOF {
		f.reader.Close()
		f.reader = nil
	}

	return n, err
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	if f.isDir {
		return 0, os.ErrInvalid
	}

	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = f.offset + offset
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

	// If we're seeking to a new position, mark the reader for reset.
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
