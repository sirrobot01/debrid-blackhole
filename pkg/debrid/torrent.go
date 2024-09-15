package debrid

import (
	"goBlack/common"
	"os"
	"path/filepath"
)

type Arr struct {
	Name  string `json:"name"`
	Token string `json:"token"`
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
	Id               string         `json:"id"`
	InfoHash         string         `json:"info_hash"`
	Name             string         `json:"name"`
	Folder           string         `json:"folder"`
	Filename         string         `json:"filename"`
	OriginalFilename string         `json:"original_filename"`
	Size             int64          `json:"size"`
	Bytes            int64          `json:"bytes"` // Size of only the files that are downloaded
	Magnet           *common.Magnet `json:"magnet"`
	Files            []TorrentFile  `json:"files"`
	Status           string         `json:"status"`
	Progress         float64        `json:"progress"`
	Speed            int64          `json:"speed"`
	Seeders          int            `json:"seeders"`

	Debrid *Debrid
	Arr    *Arr
}

func (t *Torrent) GetSymlinkFolder(parent string) string {
	return filepath.Join(parent, t.Arr.Name, t.Folder)
}

func (t *Torrent) GetMountFolder(rClonePath string) string {
	pathWithNoExt := common.RemoveExtension(t.OriginalFilename)
	if common.FileReady(filepath.Join(rClonePath, t.OriginalFilename)) {
		return t.OriginalFilename
	} else if common.FileReady(filepath.Join(rClonePath, t.Filename)) {
		return t.Filename
	} else if common.FileReady(filepath.Join(rClonePath, pathWithNoExt)) {
		return pathWithNoExt
	} else {
		return ""
	}
}

type TorrentFile struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
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
