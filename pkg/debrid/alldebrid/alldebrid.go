package alldebrid

import (
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
	gourl "net/url"
	"os"
	"path/filepath"
	"strconv"
)

type AllDebrid struct {
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

func (ad *AllDebrid) GetName() string {
	return ad.Name
}

func (ad *AllDebrid) GetLogger() zerolog.Logger {
	return ad.logger
}

func (ad *AllDebrid) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := torrent.GetLocalCache(infohashes, ad.cache)

	if len(hashes) == 0 {
		// Either all the infohashes are locally cached or none are
		ad.cache.AddMultiple(result)
		return result
	}

	// Divide hashes into groups of 100
	// AllDebrid does not support checking cached infohashes
	return result
}

func (ad *AllDebrid) SubmitMagnet(torrent *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/magnet/upload", ad.Host)
	query := gourl.Values{}
	query.Add("magnets[]", torrent.Magnet.Link)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data UploadMagnetResponse
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return nil, err
	}
	magnets := data.Data.Magnets
	if len(magnets) == 0 {
		return nil, fmt.Errorf("error adding torrent")
	}
	magnet := magnets[0]
	torrentId := strconv.Itoa(magnet.ID)
	torrent.Id = torrentId

	return torrent, nil
}

func getAlldebridStatus(statusCode int) string {
	switch {
	case statusCode == 4:
		return "downloaded"
	case statusCode >= 0 && statusCode <= 3:
		return "downloading"
	default:
		return "error"
	}
}

func flattenFiles(files []MagnetFile, parentPath string, index *int) []torrent.File {
	result := make([]torrent.File, 0)

	cfg := config.GetConfig()

	for _, f := range files {
		currentPath := f.Name
		if parentPath != "" {
			currentPath = filepath.Join(parentPath, f.Name)
		}

		if f.Elements != nil {
			// This is a folder, recurse into it
			result = append(result, flattenFiles(f.Elements, currentPath, index)...)
		} else {
			// This is a file
			fileName := filepath.Base(f.Name)

			// Skip sample files
			if utils.IsSampleFile(fileName) {
				continue
			}
			if !cfg.IsAllowedFile(fileName) {
				continue
			}

			if !cfg.IsSizeAllowed(f.Size) {
				continue
			}

			*index++
			file := torrent.File{
				Id:   strconv.Itoa(*index),
				Name: fileName,
				Size: f.Size,
				Path: currentPath,
			}
			result = append(result, file)
		}
	}

	return result
}

func (ad *AllDebrid) GetTorrent(t *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/magnet/status?id=%s", ad.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	if err != nil {
		return t, err
	}
	var res TorrentInfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		ad.logger.Info().Msgf("Error unmarshalling torrent info: %s", err)
		return t, err
	}
	data := res.Data.Magnets
	status := getAlldebridStatus(data.StatusCode)
	name := data.Filename
	t.Name = name
	t.Status = status
	t.Filename = name
	t.OriginalFilename = name
	t.Folder = name
	t.MountPath = ad.MountPath
	t.Debrid = ad.Name
	t.DownloadLinks = make(map[string]torrent.DownloadLinks)
	if status == "downloaded" {
		t.Bytes = data.Size

		t.Progress = float64((data.Downloaded / data.Size) * 100)
		t.Speed = data.DownloadSpeed
		t.Seeders = data.Seeders
		index := -1
		files := flattenFiles(data.Files, "", &index)
		t.Files = files
	}
	return t, nil
}

func (ad *AllDebrid) CheckStatus(torrent *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error) {
	for {
		tb, err := ad.GetTorrent(torrent)

		torrent = tb

		if err != nil || tb == nil {
			return tb, err
		}
		status := torrent.Status
		if status == "downloaded" {
			ad.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			if !isSymlink {
				err = ad.GetDownloadLinks(torrent)
				if err != nil {
					return torrent, err
				}
			}
			break
		} else if slices.Contains(ad.GetDownloadingStatus(), status) {
			if !torrent.DownloadUncached {
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

func (ad *AllDebrid) DeleteTorrent(torrent *torrent.Torrent) {
	url := fmt.Sprintf("%s/magnet/delete?id=%s", ad.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	_, err := ad.client.MakeRequest(req)
	if err == nil {
		ad.logger.Info().Msgf("Torrent: %s deleted", torrent.Name)
	} else {
		ad.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (ad *AllDebrid) GetDownloadLinks(t *torrent.Torrent) error {
	downloadLinks := make(map[string]torrent.DownloadLinks)
	for _, file := range t.Files {
		url := fmt.Sprintf("%s/link/unlock", ad.Host)
		query := gourl.Values{}
		query.Add("link", file.Link)
		url += "?" + query.Encode()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := ad.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data DownloadLink
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		link := data.Data.Link

		dl := torrent.DownloadLinks{
			Link:         file.Link,
			Filename:     data.Data.Filename,
			DownloadLink: link,
		}
		downloadLinks[file.Id] = dl
	}
	t.DownloadLinks = downloadLinks
	return nil
}

func (ad *AllDebrid) GetDownloadLink(t *torrent.Torrent, file *torrent.File) *torrent.DownloadLinks {
	url := fmt.Sprintf("%s/link/unlock", ad.Host)
	query := gourl.Values{}
	query.Add("link", file.Link)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	if err != nil {
		return nil
	}
	var data DownloadLink
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil
	}
	link := data.Data.Link
	return &torrent.DownloadLinks{
		DownloadLink: link,
		Link:         file.Link,
		Filename:     data.Data.Filename,
	}
}

func (ad *AllDebrid) GetCheckCached() bool {
	return ad.CheckCached
}

func (ad *AllDebrid) GetTorrents() ([]*torrent.Torrent, error) {
	return nil, nil
}

func (ad *AllDebrid) GetDownloadingStatus() []string {
	return []string{"downloading"}
}

func (ad *AllDebrid) GetDownloadUncached() bool {
	return ad.DownloadUncached
}

func New(dc config.Debrid, cache *cache.Cache) *AllDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := request.NewRLHTTPClient(rl, headers)
	return &AllDebrid{
		Name:             "alldebrid",
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
