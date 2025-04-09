package webdav

import (
	"crypto/tls"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
	"io"
	"net/http"
	"os"
	"time"
)

var sharedClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false,
	},
	Timeout: 60 * time.Second,
}

type File struct {
	cache     *debrid.Cache
	fileId    string
	torrentId string

	modTime time.Time

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

// You can not download this file because you have exceeded your traffic on this hoster

// File interface implementations for File

func (f *File) Close() error {
	if f.reader != nil {
		f.reader.Close()
		f.reader = nil
	}
	return nil
}

func (f *File) getDownloadLink() string {
	// Check if we already have a final URL cached

	if f.downloadLink != "" && isValidURL(f.downloadLink) {
		return f.downloadLink
	}
	downloadLink := f.cache.GetDownloadLink(f.torrentId, f.name, f.link)
	if downloadLink != "" && isValidURL(downloadLink) {
		return downloadLink
	}
	return ""
}

func (f *File) stream() (*http.Response, error) {
	client := sharedClient // Might be replaced with the custom client
	_log := f.cache.GetLogger()
	var (
		err          error
		downloadLink string
	)

	downloadLink = f.getDownloadLink() // Uses the first API key
	if downloadLink == "" {
		_log.Error().Msgf("Failed to get download link for %s", f.name)
		return nil, fmt.Errorf("failed to get download link")
	}

	req, err := http.NewRequest("GET", downloadLink, nil)
	if err != nil {
		_log.Error().Msgf("Failed to create HTTP request: %s", err)
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if f.offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", f.offset))
	}

	resp, err := client.Do(req)
	if err != nil {
		return resp, fmt.Errorf("HTTP request error: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {

		closeResp := func() {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		if resp.StatusCode == http.StatusServiceUnavailable {
			closeResp()
			// Read the body to consume the response
			f.cache.MarkDownloadLinkAsInvalid(downloadLink, "bandwidth_exceeded")
			// Retry with a different API key if it's available
			return f.stream()

		} else if resp.StatusCode == http.StatusNotFound {
			closeResp()
			// Mark download link as not found
			// Regenerate a new download link
			f.cache.MarkDownloadLinkAsInvalid(downloadLink, "link_not_found")
			f.cache.RemoveDownloadLink(f.link) // Remove the link from the cache
			// Generate a new download link
			downloadLink = f.getDownloadLink()
			if downloadLink == "" {
				_log.Error().Msgf("Failed to get download link for %s", f.name)
				return nil, fmt.Errorf("failed to get download link")
			}
			req, err = http.NewRequest("GET", downloadLink, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create HTTP request: %w", err)
			}
			if f.offset > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", f.offset))
			}

			resp, err = client.Do(req)
			if err != nil {
				return resp, fmt.Errorf("HTTP request error: %w", err)
			}
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
				closeResp()
				// Read the body to consume the response
				f.cache.MarkDownloadLinkAsInvalid(downloadLink, "link_not_found")
				return resp, fmt.Errorf("link not found")
			}
			return resp, nil

		} else {
			closeResp()
			return resp, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
		}

	}
	return resp, nil
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
		if f.reader != nil && f.seekPending {
			f.reader.Close()
			f.reader = nil
		}

		// Make the request to get the file
		resp, err := f.stream()
		if err != nil {
			return 0, err
		}
		if resp == nil {
			return 0, fmt.Errorf("failed to get response")
		}

		f.reader = resp.Body
		f.seekPending = false
	}

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
			modTime: f.modTime,
			isDir:   true,
		}, nil
	}

	return &FileInfo{
		name:    f.name,
		size:    f.size,
		mode:    0644,
		modTime: f.modTime,
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
