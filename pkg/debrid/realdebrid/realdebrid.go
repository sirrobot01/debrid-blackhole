package realdebrid

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
	"net/http"
	gourl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type RealDebrid struct {
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

func (r *RealDebrid) GetName() string {
	return r.Name
}

func (r *RealDebrid) GetLogger() zerolog.Logger {
	return r.logger
}

// GetTorrentFiles returns a list of torrent files from the torrent info
// validate is used to determine if the files should be validated
// if validate is false, selected files will be returned
func GetTorrentFiles(data TorrentInfo, validate bool) []torrent.File {
	files := make([]torrent.File, 0)
	cfg := config.GetConfig()
	idx := 0
	for _, f := range data.Files {

		name := filepath.Base(f.Path)

		if validate {
			if utils.RegexMatch(utils.SAMPLEMATCH, name) {
				// Skip sample files
				continue
			}
			if !cfg.IsAllowedFile(name) {
				continue
			}
			if !cfg.IsSizeAllowed(f.Bytes) {
				continue
			}
		} else {
			if f.Selected == 0 {
				continue
			}
		}

		fileId := f.ID
		_link := ""
		if len(data.Links) > idx {
			_link = data.Links[idx]
		}
		file := torrent.File{
			Name: name,
			Path: name,
			Size: f.Bytes,
			Id:   strconv.Itoa(fileId),
			Link: _link,
		}
		files = append(files, file)
		idx++
	}
	return files
}

func (r *RealDebrid) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := torrent.GetLocalCache(infohashes, r.cache)

	if len(hashes) == 0 {
		// Either all the infohashes are locally cached or none are
		r.cache.AddMultiple(result)
		return result
	}

	// Divide hashes into groups of 100
	for i := 0; i < len(hashes); i += 200 {
		end := i + 200
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

		hashStr := strings.Join(validHashes, "/")
		url := fmt.Sprintf("%s/torrents/instantAvailability/%s", r.Host, hashStr)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			r.logger.Info().Msgf("Error checking availability: %v", err)
			return result
		}
		var data AvailabilityResponse
		err = json.Unmarshal(resp, &data)
		if err != nil {
			r.logger.Info().Msgf("Error marshalling availability: %v", err)
			return result
		}
		for _, h := range hashes[i:end] {
			hosters, exists := data[strings.ToLower(h)]
			if exists && len(hosters.Rd) > 0 {
				result[h] = true
			}
		}
	}
	r.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (r *RealDebrid) SubmitMagnet(t *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/torrents/addMagnet", r.Host)
	payload := gourl.Values{
		"magnet": {t.Magnet.Link},
	}
	var data AddMagnetSchema
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}
	t.Id = data.Id
	t.Debrid = r.Name
	t.MountPath = r.MountPath
	return t, nil
}

func (r *RealDebrid) GetTorrent(t *torrent.Torrent) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return t, err
	}
	var data TorrentInfo
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return t, err
	}
	name := utils.RemoveInvalidChars(data.OriginalFilename)
	t.Name = name
	t.Bytes = data.Bytes
	t.Folder = name
	t.Progress = data.Progress
	t.Status = data.Status
	t.Speed = data.Speed
	t.Seeders = data.Seeders
	t.Filename = data.Filename
	t.OriginalFilename = data.OriginalFilename
	t.Links = data.Links
	t.MountPath = r.MountPath
	t.Debrid = r.Name
	t.DownloadLinks = make(map[string]torrent.DownloadLinks)
	files := GetTorrentFiles(data, false) // Get selected files
	t.Files = files
	return t, nil
}

func (r *RealDebrid) CheckStatus(t *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for {
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			r.logger.Info().Msgf("ERROR Checking file: %v", err)
			return t, err
		}
		var data TorrentInfo
		if err = json.Unmarshal(resp, &data); err != nil {
			return t, err
		}
		status := data.Status
		name := utils.RemoveInvalidChars(data.OriginalFilename)
		t.Name = name // Important because some magnet changes the name
		t.Folder = name
		t.Filename = data.Filename
		t.OriginalFilename = data.OriginalFilename
		t.Bytes = data.Bytes
		t.Progress = data.Progress
		t.Speed = data.Speed
		t.Seeders = data.Seeders
		t.Links = data.Links
		t.Status = status
		t.Debrid = r.Name
		t.MountPath = r.MountPath
		if status == "waiting_files_selection" {
			files := GetTorrentFiles(data, true) // Validate files to be selected
			t.Files = files
			if len(files) == 0 {
				return t, fmt.Errorf("no video files found")
			}
			filesId := make([]string, 0)
			for _, f := range files {
				filesId = append(filesId, f.Id)
			}
			p := gourl.Values{
				"files": {strings.Join(filesId, ",")},
			}
			payload := strings.NewReader(p.Encode())
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/torrents/selectFiles/%s", r.Host, t.Id), payload)
			_, err = r.client.MakeRequest(req)
			if err != nil {
				return t, err
			}
		} else if status == "downloaded" {
			files := GetTorrentFiles(data, false) // Get selected files
			t.Files = files
			r.logger.Info().Msgf("Torrent: %s downloaded to RD", t.Name)
			if !isSymlink {
				err = r.GetDownloadLinks(t)
				if err != nil {
					return t, err
				}
			}
			break
		} else if slices.Contains(r.GetDownloadingStatus(), status) {
			if !r.DownloadUncached && !t.DownloadUncached {
				return t, fmt.Errorf("torrent: %s not cached", t.Name)
			}
			// Break out of the loop if the torrent is downloading.
			// This is necessary to prevent infinite loop since we moved to sync downloading and async processing
			break
		} else {
			return t, fmt.Errorf("torrent: %s has error: %s", t.Name, status)
		}

	}
	return t, nil
}

func (r *RealDebrid) DeleteTorrent(torrent *torrent.Torrent) {
	url := fmt.Sprintf("%s/torrents/delete/%s", r.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Info().Msgf("Torrent: %s deleted", torrent.Name)
	} else {
		r.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (r *RealDebrid) GetDownloadLinks(t *torrent.Torrent) error {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	downloadLinks := make(map[string]torrent.DownloadLinks)
	for _, f := range t.Files {
		dlLink := t.DownloadLinks[f.Id]
		if f.Link == "" || dlLink.DownloadLink != "" {
			continue
		}
		payload := gourl.Values{
			"link": {f.Link},
		}
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data UnrestrictResponse
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		download := torrent.DownloadLinks{
			Link:         data.Link,
			Filename:     data.Filename,
			DownloadLink: data.Download,
		}
		downloadLinks[f.Id] = download
	}
	t.DownloadLinks = downloadLinks
	return nil
}

func (r *RealDebrid) GetDownloadLink(t *torrent.Torrent, file *torrent.File) *torrent.DownloadLinks {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	payload := gourl.Values{
		"link": {file.Link},
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil
	}
	var data UnrestrictResponse
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil
	}
	return &torrent.DownloadLinks{
		Link:         data.Link,
		Filename:     data.Filename,
		DownloadLink: data.Download,
	}
}

func (r *RealDebrid) GetCheckCached() bool {
	return r.CheckCached
}

func (r *RealDebrid) getTorrents(offset int, limit int) ([]*torrent.Torrent, error) {
	url := fmt.Sprintf("%s/torrents?limit=%d", r.Host, limit)
	if offset > 0 {
		url = fmt.Sprintf("%s&offset=%d", url, offset)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data []TorrentsResponse
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}
	torrents := make([]*torrent.Torrent, 0)
	for _, t := range data {

		torrents = append(torrents, &torrent.Torrent{
			Id:               t.Id,
			Name:             t.Filename,
			Bytes:            t.Bytes,
			Progress:         t.Progress,
			Status:           t.Status,
			Filename:         t.Filename,
			OriginalFilename: t.Filename,
			Links:            t.Links,
		})
	}
	return torrents, nil
}

func (r *RealDebrid) GetTorrents() ([]*torrent.Torrent, error) {
	torrents := make([]*torrent.Torrent, 0)
	offset := 0
	limit := 5000
	for {
		ts, err := r.getTorrents(offset, limit)
		if err != nil {
			break
		}
		if len(ts) == 0 {
			break
		}
		torrents = append(torrents, ts...)
		offset = len(torrents)
	}
	return torrents, nil

}

func (r *RealDebrid) GetDownloadingStatus() []string {
	return []string{"downloading", "magnet_conversion", "queued", "compressing", "uploading"}
}

func New(dc config.Debrid, cache *cache.Cache) *RealDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := request.NewRLHTTPClient(rl, headers)
	return &RealDebrid{
		Name:             "realdebrid",
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
