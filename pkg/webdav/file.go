package webdav

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/cache"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"io"
	"net/http"
	"os"
	"time"
)

type File struct {
	cache         *cache.Cache
	cachedTorrent *cache.CachedTorrent
	file          *torrent.File
	offset        int64
	isDir         bool
	children      []os.FileInfo
	reader        io.ReadCloser
}

// File interface implementations for File

func (f *File) Close() error {
	return nil
}

func (f *File) GetDownloadLink() string {
	file := f.file
	link, err := f.cache.GetFileDownloadLink(f.cachedTorrent, file)
	if err != nil {
		return ""
	}
	return link
}

func (f *File) Read(p []byte) (n int, err error) {
	// Directories cannot be read as a byte stream.
	if f.isDir {
		return 0, os.ErrInvalid
	}

	// If we haven't started streaming the file yet, open the HTTP connection.
	if f.reader == nil {
		// Create an HTTP GET request to the file's URL.
		req, err := http.NewRequest("GET", f.GetDownloadLink(), nil)
		if err != nil {
			return 0, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		// If we've already read some data (f.offset > 0), request only the remaining bytes.
		if f.offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", f.offset))
		}

		// Execute the HTTP request.
		resp, err := http.DefaultClient.Do(req)
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

	switch whence {
	case io.SeekStart:
		f.offset = offset
	case io.SeekCurrent:
		f.offset += offset
	case io.SeekEnd:
		f.offset = f.file.Size - offset
	default:
		return 0, os.ErrInvalid
	}

	if f.offset < 0 {
		f.offset = 0
	}
	if f.offset > f.file.Size {
		f.offset = f.file.Size
	}

	return f.offset, nil
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

func (f *File) Stat() (os.FileInfo, error) {
	if f.isDir {
		name := "/"
		if f.cachedTorrent != nil {
			name = f.cachedTorrent.Name
		}
		return &FileInfo{
			name:    name,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: time.Now(),
			isDir:   true,
		}, nil
	}

	return &FileInfo{
		name:    f.file.Name,
		size:    f.file.Size,
		mode:    0644,
		modTime: time.Now(),
		isDir:   false,
	}, nil
}

func (f *File) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}
