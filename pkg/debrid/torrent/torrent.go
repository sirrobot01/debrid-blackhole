package torrent

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/cache"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"os"
	"path/filepath"
	"sync"
)

type Torrent struct {
	Id               string                   `json:"id"`
	InfoHash         string                   `json:"info_hash"`
	Name             string                   `json:"name"`
	Folder           string                   `json:"folder"`
	Filename         string                   `json:"filename"`
	OriginalFilename string                   `json:"original_filename"`
	Size             int64                    `json:"size"`
	Bytes            int64                    `json:"bytes"` // Size of only the files that are downloaded
	Magnet           *utils.Magnet            `json:"magnet"`
	Files            []File                   `json:"files"`
	Status           string                   `json:"status"`
	Added            string                   `json:"added"`
	Progress         float64                  `json:"progress"`
	Speed            int64                    `json:"speed"`
	Seeders          int                      `json:"seeders"`
	Links            []string                 `json:"links"`
	DownloadLinks    map[string]DownloadLinks `json:"download_links"`
	MountPath        string                   `json:"mount_path"`

	Debrid string `json:"debrid"`

	Arr              *arr.Arr   `json:"arr"`
	Mu               sync.Mutex `json:"-"`
	SizeDownloaded   int64      `json:"-"` // This is used for local download
	DownloadUncached bool       `json:"-"`
}

type DownloadLinks struct {
	Filename     string `json:"filename"`
	Link         string `json:"link"`
	DownloadLink string `json:"download_link"`
}

func (t *Torrent) GetSymlinkFolder(parent string) string {
	return filepath.Join(parent, t.Arr.Name, t.Folder)
}

func (t *Torrent) GetMountFolder(rClonePath string) (string, error) {
	_log := logger.GetDefaultLogger()
	possiblePaths := []string{
		t.OriginalFilename,
		t.Filename,
		utils.RemoveExtension(t.OriginalFilename),
	}

	for _, path := range possiblePaths {
		_p := filepath.Join(rClonePath, path)
		_log.Trace().Msgf("Checking path: %s", _p)
		_, err := os.Stat(_p)
		if !os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no path found")
}

type File struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
	Link string `json:"link"`
}

func (t *Torrent) Cleanup(remove bool) {
	if remove {
		err := os.Remove(t.Filename)
		if err != nil {
			return
		}
	}
}

func (t *Torrent) GetFile(id string) *File {
	for _, f := range t.Files {
		if f.Id == id {
			return &f
		}
	}
	return nil
}

func GetLocalCache(infohashes []string, cache *cache.Cache) ([]string, map[string]bool) {
	result := make(map[string]bool)
	hashes := make([]string, 0)

	if len(infohashes) == 0 {
		return hashes, result
	}
	if len(infohashes) == 1 {
		if cache.Exists(infohashes[0]) {
			return hashes, map[string]bool{infohashes[0]: true}
		}
		return infohashes, result
	}

	cachedHashes := cache.GetMultiple(infohashes)
	for _, h := range infohashes {
		_, exists := cachedHashes[h]
		if !exists {
			hashes = append(hashes, h)
		} else {
			result[h] = true
		}
	}

	return infohashes, result
}
