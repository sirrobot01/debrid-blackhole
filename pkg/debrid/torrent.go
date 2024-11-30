package debrid

import (
	"goBlack/common"
	"goBlack/pkg/arr"
	"os"
	"path/filepath"
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
	Magnet           *common.Magnet         `json:"magnet"`
	Files            []TorrentFile          `json:"files"`
	Status           string                 `json:"status"`
	Added            string                 `json:"added"`
	Progress         float64                `json:"progress"`
	Speed            int                    `json:"speed"`
	Seeders          int                    `json:"seeders"`
	Links            []string               `json:"links"`
	DownloadLinks    []TorrentDownloadLinks `json:"download_links"`

	Debrid Service
	Arr    *arr.Arr
}

type TorrentDownloadLinks struct {
	Filename     string `json:"filename"`
	Link         string `json:"link"`
	DownloadLink string `json:"download_link"`
}

func (t *Torrent) GetSymlinkFolder(parent string) string {
	return filepath.Join(parent, t.Arr.Name, t.Folder)
}

func (t *Torrent) GetMountFolder(rClonePath string) string {
	possiblePaths := []string{
		t.OriginalFilename,
		t.Filename,
		common.RemoveExtension(t.OriginalFilename),
	}

	for _, path := range possiblePaths {
		if path != "" && common.FileReady(filepath.Join(rClonePath, path)) {
			return path
		}
	}
	return ""
}

func (t *Torrent) Delete() {
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
