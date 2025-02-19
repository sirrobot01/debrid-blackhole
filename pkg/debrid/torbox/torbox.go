package torbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"

	"mime/multipart"
	"net/http"
	gourl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type Torbox struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *request.RLHTTPClient
	cache            *common.Cache
	MountPath        string
	logger           zerolog.Logger
	CheckCached      bool
}

func (tb *Torbox) GetName() string {
	return tb.Name
}

func (tb *Torbox) GetLogger() zerolog.Logger {
	return tb.logger
}

func (tb *Torbox) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := torrent.GetLocalCache(infohashes, tb.cache)

	if len(hashes) == 0 {
		// Either all the infohashes are locally cached or none are
		tb.cache.AddMultiple(result)
		return result
	}

	// Divide hashes into groups of 100
	for i := 0; i < len(hashes); i += 100 {
		end := i + 100
		if end > len(hashes) {
			end = len(hashes)
		}

		// Filter out empty strings
		validHashes := make([]string, 0, end-i)
		for _, hash := range hashes[i:end] {
			if hash != "" {
				validHashes = append(validHashes, hash)
			}
		}

		// If no valid hashes in this batch, continue to the next batch
		if len(validHashes) == 0 {
			continue
		}

		hashStr := strings.Join(validHashes, ",")
		url := fmt.Sprintf("%s/api/torrents/checkcached?hash=%s", tb.Host, hashStr)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := tb.client.MakeRequest(req)
		if err != nil {
			tb.logger.Info().Msgf("Error checking availability: %v", err)
			return result
		}
		var res AvailableResponse
		err = json.Unmarshal(resp, &res)
		if err != nil {
			tb.logger.Info().Msgf("Error marshalling availability: %v", err)
			return result
		}
		if res.Data == nil {
			return result
		}

		for h, cache := range *res.Data {
			if cache.Size > 0 {
				result[strings.ToUpper(h)] = true
			}
		}
	}
	tb.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (tb *Torbox) SubmitMagnet(torrent *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/api/torrents/createtorrent", tb.Host)
	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	_ = writer.WriteField("magnet", torrent.Magnet.Link)
	err := writer.Close()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, url, payload)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := tb.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data AddMagnetResponse
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return nil, err
	}
	if data.Data == nil {
		return nil, fmt.Errorf("error adding torrent")
	}
	dt := *data.Data
	torrentId := strconv.Itoa(dt.Id)
	torrent.Id = torrentId
	torrent.MountPath = tb.MountPath
	torrent.Debrid = tb.Name

	return torrent, nil
}

func getTorboxStatus(status string, finished bool) string {
	if finished {
		return "downloaded"
	}
	downloading := []string{"completed", "cached", "paused", "downloading", "uploading",
		"checkingResumeData", "metaDL", "pausedUP", "queuedUP", "checkingUP",
		"forcedUP", "allocating", "downloading", "metaDL", "pausedDL",
		"queuedDL", "checkingDL", "forcedDL", "checkingResumeData", "moving"}
	switch {
	case slices.Contains(downloading, status):
		return "downloading"
	default:
		return "error"
	}
}

func (tb *Torbox) GetTorrent(id string) (*torrent.Torrent, error) {
	t := &torrent.Torrent{}
	url := fmt.Sprintf("%s/api/torrents/mylist/?id=%s", tb.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := tb.client.MakeRequest(req)
	if err != nil {
		return t, err
	}
	var res InfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return t, err
	}
	data := res.Data
	name := data.Name
	t.Id = id
	t.Name = name
	t.Bytes = data.Size
	t.Folder = name
	t.Progress = data.Progress * 100
	t.Status = getTorboxStatus(data.DownloadState, data.DownloadFinished)
	t.Speed = data.DownloadSpeed
	t.Seeders = data.Seeds
	t.Filename = name
	t.OriginalFilename = name
	t.MountPath = tb.MountPath
	t.Debrid = tb.Name
	t.DownloadLinks = make(map[string]torrent.DownloadLinks)
	files := make([]torrent.File, 0)
	cfg := config.GetConfig()
	for _, f := range data.Files {
		fileName := filepath.Base(f.Name)
		if utils.IsSampleFile(fileName) {
			// Skip sample files
			continue
		}
		if !cfg.IsAllowedFile(fileName) {
			continue
		}

		if !cfg.IsSizeAllowed(f.Size) {
			continue
		}
		file := torrent.File{
			Id:   strconv.Itoa(f.Id),
			Name: fileName,
			Size: f.Size,
			Path: fileName,
		}
		files = append(files, file)
	}
	var cleanPath string
	if len(files) > 0 {
		cleanPath = path.Clean(data.Files[0].Name)
	} else {
		cleanPath = path.Clean(data.Name)
	}

	t.OriginalFilename = strings.Split(cleanPath, "/")[0]
	t.Files = files
	//t.Debrid = tb
	return t, nil
}

func (tb *Torbox) CheckStatus(torrent *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error) {
	for {
		t, err := tb.GetTorrent(torrent.Id)

		torrent = t

		if err != nil || t == nil {
			return t, err
		}
		status := torrent.Status
		if status == "downloaded" {
			tb.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			if !isSymlink {
				err = tb.GetDownloadLinks(torrent)
				if err != nil {
					return torrent, err
				}
			}
			break
		} else if status == "downloading" {
			if !tb.DownloadUncached {
				go tb.DeleteTorrent(torrent)
				return torrent, fmt.Errorf("torrent: %s not cached", torrent.Name)
			}
			// Break out of the loop if the torrent is downloading.
			// This is necessary to prevent infinite loop since we moved to sync downloading and async processing
			break
		} else {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		}

	}
	return torrent, nil
}

func (tb *Torbox) DeleteTorrent(torrent *torrent.Torrent) {
	url := fmt.Sprintf("%s/api/torrents/controltorrent/%s", tb.Host, torrent.Id)
	payload := map[string]string{"torrent_id": torrent.Id, "action": "Delete"}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodDelete, url, bytes.NewBuffer(jsonPayload))
	_, err := tb.client.MakeRequest(req)
	if err == nil {
		tb.logger.Info().Msgf("Torrent: %s deleted", torrent.Name)
	} else {
		tb.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (tb *Torbox) GetDownloadLinks(t *torrent.Torrent) error {
	downloadLinks := make(map[string]torrent.DownloadLinks)
	for _, file := range t.Files {
		url := fmt.Sprintf("%s/api/torrents/requestdl/", tb.Host)
		query := gourl.Values{}
		query.Add("torrent_id", t.Id)
		query.Add("token", tb.APIKey)
		query.Add("file_id", file.Id)
		url += "?" + query.Encode()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := tb.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data DownloadLinksResponse
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		if data.Data == nil {
			return fmt.Errorf("error getting download links")
		}
		idx := 0
		link := *data.Data

		dl := torrent.DownloadLinks{
			Link:         link,
			Filename:     t.Files[idx].Name,
			DownloadLink: link,
		}
		downloadLinks[file.Id] = dl
	}
	t.DownloadLinks = downloadLinks
	return nil
}

func (tb *Torbox) GetDownloadLink(t *torrent.Torrent, file *torrent.File) *torrent.DownloadLinks {
	url := fmt.Sprintf("%s/api/torrents/requestdl/", tb.Host)
	query := gourl.Values{}
	query.Add("torrent_id", t.Id)
	query.Add("token", tb.APIKey)
	query.Add("file_id", file.Id)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := tb.client.MakeRequest(req)
	if err != nil {
		return nil
	}
	var data DownloadLinksResponse
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil
	}
	if data.Data == nil {
		return nil
	}
	link := *data.Data
	return &torrent.DownloadLinks{
		Link:         file.Link,
		Filename:     file.Name,
		DownloadLink: link,
	}
}

func (tb *Torbox) GetCheckCached() bool {
	return tb.CheckCached
}

func (tb *Torbox) GetTorrents() ([]*torrent.Torrent, error) {
	return nil, fmt.Errorf("not implemented")
}

func New(dc config.Debrid, cache *common.Cache) *Torbox {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := request.NewRLHTTPClient(rl, headers)
	return &Torbox{
		Name:             "torbox",
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		cache:            cache,
		MountPath:        dc.Folder,
		logger:           logger.NewLogger(dc.Name, config.GetConfig().LogLevel, os.Stdout),
		CheckCached:      dc.CheckCached,
	}
}
