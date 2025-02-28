package debrid_link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/cache"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"slices"

	"net/http"
	"os"
	"strings"
)

type DebridLink struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *request.RLHTTPClient
	cache            *cache.Cache
	MountPath        string
	logger           zerolog.Logger
	CheckCached      bool
}

func (dl *DebridLink) GetName() string {
	return dl.Name
}

func (dl *DebridLink) GetLogger() zerolog.Logger {
	return dl.logger
}

func (dl *DebridLink) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := torrent.GetLocalCache(infohashes, dl.cache)

	if len(hashes) == 0 {
		// Either all the infohashes are locally cached or none are
		dl.cache.AddMultiple(result)
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
		url := fmt.Sprintf("%s/seedbox/cached/%s", dl.Host, hashStr)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := dl.client.MakeRequest(req)
		if err != nil {
			dl.logger.Info().Msgf("Error checking availability: %v", err)
			return result
		}
		var data AvailableResponse
		err = json.Unmarshal(resp, &data)
		if err != nil {
			dl.logger.Info().Msgf("Error marshalling availability: %v", err)
			return result
		}
		if data.Value == nil {
			return result
		}
		value := *data.Value
		for _, h := range hashes[i:end] {
			_, exists := value[h]
			if exists {
				result[h] = true
			}
		}
	}
	dl.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (dl *DebridLink) GetTorrent(id string) (*torrent.Torrent, error) {
	t := &torrent.Torrent{}
	url := fmt.Sprintf("%s/seedbox/list?ids=%s", dl.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := dl.client.MakeRequest(req)
	if err != nil {
		return t, err
	}
	var res TorrentInfo
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return t, err
	}
	if !res.Success {
		return t, fmt.Errorf("error getting torrent")
	}
	if res.Value == nil {
		return t, fmt.Errorf("torrent not found")
	}
	dt := *res.Value

	if len(dt) == 0 {
		return t, fmt.Errorf("torrent not found")
	}
	data := dt[0]
	status := "downloading"
	if data.Status == 100 {
		status = "downloaded"
	}
	name := utils.RemoveInvalidChars(data.Name)
	t.Id = data.ID
	t.Name = name
	t.Bytes = data.TotalSize
	t.Folder = name
	t.Progress = data.DownloadPercent
	t.Status = status
	t.Speed = data.DownloadSpeed
	t.Seeders = data.PeersConnected
	t.Filename = name
	t.OriginalFilename = name
	files := make([]torrent.File, len(data.Files))
	cfg := config.GetConfig()
	for i, f := range data.Files {
		if !cfg.IsSizeAllowed(f.Size) {
			continue
		}
		files[i] = torrent.File{
			Id:   f.ID,
			Name: f.Name,
			Size: f.Size,
			Path: f.Name,
		}
	}
	t.Files = files
	return t, nil
}

func (dl *DebridLink) SubmitMagnet(t *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/seedbox/add", dl.Host)
	payload := map[string]string{"url": t.Magnet.Link}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonPayload))
	resp, err := dl.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var res SubmitTorrentInfo
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return nil, err
	}
	if !res.Success || res.Value == nil {
		return nil, fmt.Errorf("error adding torrent")
	}
	data := *res.Value
	status := "downloading"
	name := utils.RemoveInvalidChars(data.Name)
	t.Id = data.ID
	t.Name = name
	t.Bytes = data.TotalSize
	t.Folder = name
	t.Progress = data.DownloadPercent
	t.Status = status
	t.Speed = data.DownloadSpeed
	t.Seeders = data.PeersConnected
	t.Filename = name
	t.OriginalFilename = name
	t.MountPath = dl.MountPath
	t.Debrid = dl.Name
	t.DownloadLinks = make(map[string]torrent.DownloadLinks)
	files := make([]torrent.File, len(data.Files))
	for i, f := range data.Files {
		files[i] = torrent.File{
			Id:   f.ID,
			Name: f.Name,
			Size: f.Size,
			Path: f.Name,
			Link: f.DownloadURL,
		}
	}
	t.Files = files

	return t, nil
}

func (dl *DebridLink) CheckStatus(torrent *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error) {
	for {
		t, err := dl.GetTorrent(torrent.Id)
		torrent = t
		if err != nil || torrent == nil {
			return torrent, err
		}
		status := torrent.Status
		if status == "downloaded" {
			dl.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			err = dl.GetDownloadLinks(torrent)
			if err != nil {
				return torrent, err
			}
			break
		} else if slices.Contains(dl.GetDownloadingStatus(), status) {
			if !dl.DownloadUncached {
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

func (dl *DebridLink) DeleteTorrent(torrent *torrent.Torrent) {
	url := fmt.Sprintf("%s/seedbox/%s/remove", dl.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := dl.client.MakeRequest(req)
	if err == nil {
		dl.logger.Info().Msgf("Torrent: %s deleted", torrent.Name)
	} else {
		dl.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (dl *DebridLink) GetDownloadLinks(t *torrent.Torrent) error {
	downloadLinks := make(map[string]torrent.DownloadLinks)
	for _, f := range t.Files {
		dl := torrent.DownloadLinks{
			Link:     f.Link,
			Filename: f.Name,
		}
		downloadLinks[f.Id] = dl
	}
	t.DownloadLinks = downloadLinks
	return nil
}

func (dl *DebridLink) GetDownloadLink(t *torrent.Torrent, file *torrent.File) *torrent.DownloadLinks {
	dlLink, ok := t.DownloadLinks[file.Id]
	if !ok {
		return nil
	}
	return &dlLink
}

func (dl *DebridLink) GetDownloadingStatus() []string {
	return []string{"downloading"}
}

func (dl *DebridLink) GetCheckCached() bool {
	return dl.CheckCached
}

func New(dc config.Debrid, cache *cache.Cache) *DebridLink {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
		"Content-Type":  "application/json",
	}
	client := request.NewRLHTTPClient(rl, headers)
	return &DebridLink{
		Name:             "debridlink",
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

func (dl *DebridLink) GetTorrents() ([]*torrent.Torrent, error) {
	return nil, fmt.Errorf("not implemented")
}
