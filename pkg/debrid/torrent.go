package debrid

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"os"
	"path/filepath"
	"sync"
)

type Arr struct {
	Name  string `json:"name"`
	Token string `json:"-"`
	Host  string `json:"host"`
}

type ArrHistorySchema struct {
	Page          int    `json:"page"`
	PageSize      int    `json:"pageSize"`
	SortKey       string `json:"sortKey"`
	SortDirection string `json:"sortDirection"`
	TotalRecords  int    `json:"totalRecords"`
	Records       []struct {
		ID         int    `json:"id"`
		DownloadID string `json:"downloadId"`
	} `json:"records"`
}

type Torrent struct {
	Id               string                 `json:"id"`
	InfoHash         string                 `json:"info_hash"`
	Name             string                 `json:"name"`
	Folder           string                 `json:"folder"`
	Filename         string                 `json:"filename"`
	OriginalFilename string                 `json:"original_filename"`
	Size             int64                  `json:"size"`
	Bytes            int64                  `json:"bytes"` // Size of only the files that are downloaded
	Magnet           *utils.Magnet          `json:"magnet"`
	Files            []TorrentFile          `json:"files"`
	Status           string                 `json:"status"`
	Added            string                 `json:"added"`
	Progress         float64                `json:"progress"`
	Speed            int                    `json:"speed"`
	Seeders          int                    `json:"seeders"`
	Links            []string               `json:"links"`
	DownloadLinks    []TorrentDownloadLinks `json:"download_links"`

	Debrid         Service    `json:"-"`
	Arr            *arr.Arr   `json:"arr"`
	Mu             sync.Mutex `json:"-"`
	SizeDownloaded int64      `json:"-"` // This is used for local download
}

type TorrentDownloadLinks struct {
	Filename     string `json:"filename"`
	Link         string `json:"link"`
	DownloadLink string `json:"download_link"`
}

func (t *Torrent) GetSymlinkFolder(parent string) string {
	return filepath.Join(parent, t.Arr.Name, t.Folder)
}

func (t *Torrent) GetMountFolder(rClonePath string) (string, error) {
	possiblePaths := []string{
		t.OriginalFilename,
		t.Filename,
		common.RemoveExtension(t.OriginalFilename),
	}

	for _, path := range possiblePaths {
		_, err := os.Stat(filepath.Join(rClonePath, path))
		if !os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no path found")
}

func (t *Torrent) Delete() {
	if t.Debrid == nil {
		return
	}
	t.Debrid.DeleteTorrent(t)

}

type TorrentFile struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
	Link string `json:"link"`
}

func getEventId(eventType string) int {
	switch eventType {
	case "grabbed":
		return 1
	case "seriesFolderDownloaded":
		return 2
	case "DownloadFolderImported":
		return 3
	case "DownloadFailed":
		return 4
	case "DownloadIgnored":
		return 7
	default:
		return 0
	}
}

func (t *Torrent) Cleanup(remove bool) {
	if remove {
		err := os.Remove(t.Filename)
		if err != nil {
			return
		}
	}
}
