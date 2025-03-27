package realdebrid

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"io"
	"net/http"
	gourl "net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RealDebrid struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *request.Client

	MountPath   string
	logger      zerolog.Logger
	CheckCached bool
}

func (r *RealDebrid) GetName() string {
	return r.Name
}

func (r *RealDebrid) GetLogger() zerolog.Logger {
	return r.logger
}

// getTorrentFiles returns a list of torrent files from the torrent info
// validate is used to determine if the files should be validated
// if validate is false, selected files will be returned
func getTorrentFiles(t *types.Torrent, data TorrentInfo, validate bool) map[string]types.File {
	files := make(map[string]types.File)
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

		file := types.File{
			Name: name,
			Path: name,
			Size: f.Bytes,
			Id:   strconv.Itoa(fileId),
			Link: _link,
		}
		files[name] = file
		idx++
	}
	return files
}

func (r *RealDebrid) IsAvailable(hashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	result := make(map[string]bool)

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
	return result
}

func (r *RealDebrid) SubmitMagnet(t *types.Torrent) (*types.Torrent, error) {
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

func (r *RealDebrid) UpdateTorrent(t *types.Torrent) error {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return err
	}
	var data TorrentInfo
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return err
	}
	t.Name = data.Filename
	t.Bytes = data.Bytes
	t.Folder = data.OriginalFilename
	t.Progress = data.Progress
	t.Status = data.Status
	t.Speed = data.Speed
	t.Seeders = data.Seeders
	t.Filename = data.Filename
	t.OriginalFilename = data.OriginalFilename
	t.Links = data.Links
	t.MountPath = r.MountPath
	t.Debrid = r.Name
	t.Added = data.Added
	t.Files = getTorrentFiles(t, data, false) // Get selected files
	return nil
}

func (r *RealDebrid) CheckStatus(t *types.Torrent, isSymlink bool) (*types.Torrent, error) {
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
		t.Name = data.Filename // Important because some magnet changes the name
		t.Folder = data.OriginalFilename
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
			t.Files = getTorrentFiles(t, data, true)
			if len(t.Files) == 0 {
				return t, fmt.Errorf("no video files found")
			}
			filesId := make([]string, 0)
			for _, f := range t.Files {
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
			t.Files = getTorrentFiles(t, data, false) // Get selected files
			r.logger.Info().Msgf("Torrent: %s downloaded to RD", t.Name)
			if !isSymlink {
				err = r.GenerateDownloadLinks(t)
				if err != nil {
					return t, err
				}
			}
			break
		} else if slices.Contains(r.GetDownloadingStatus(), status) {
			if !t.DownloadUncached {
				return t, fmt.Errorf("torrent: %s not cached", t.Name)
			}
		} else {
			return t, fmt.Errorf("torrent: %s has error: %s", t.Name, status)
		}

	}
	return t, nil
}

func (r *RealDebrid) DeleteTorrent(torrentId string) {
	url := fmt.Sprintf("%s/torrents/delete/%s", r.Host, torrentId)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Info().Msgf("Torrent: %s deleted", torrentId)
	} else {
		r.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (r *RealDebrid) GenerateDownloadLinks(t *types.Torrent) error {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	filesCh := make(chan types.File, len(t.Files))
	errCh := make(chan error, len(t.Files))

	var wg sync.WaitGroup

	for _, f := range t.Files {
		wg.Add(1)
		go func(file types.File) {
			defer wg.Done()

			payload := gourl.Values{"link": {file.Link}}
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
			if err != nil {
				errCh <- err
				return
			}

			resp, err := r.client.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			if resp.StatusCode == http.StatusServiceUnavailable {
				errCh <- request.HosterUnavailableError
				return
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)

			var data UnrestrictResponse
			if err = json.Unmarshal(b, &data); err != nil {
				errCh <- err
				return
			}

			file.DownloadLink = data.Download
			filesCh <- file
		}(f)
	}

	go func() {
		wg.Wait()
		close(filesCh)
		close(errCh)
	}()

	// Collect results
	files := make(map[string]types.File, len(t.Files))
	for file := range filesCh {
		files[file.Name] = file
	}

	// Check for errors
	for err := range errCh {
		if err != nil {
			return err // Return the first error encountered
		}
	}

	t.Files = files
	return nil
}

func (r *RealDebrid) CheckLink(link string) error {
	url := fmt.Sprintf("%s/unrestrict/check", r.Host)
	payload := gourl.Values{
		"link": {link},
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return request.ErrLinkBroken // File has been removed
	}
	return nil
}

func (r *RealDebrid) GetDownloadLink(t *types.Torrent, file *types.File) (string, error) {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	payload := gourl.Values{
		"link": {file.Link},
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return "", request.HosterUnavailableError
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var data UnrestrictResponse
	if err = json.Unmarshal(b, &data); err != nil {
		return "", err
	}
	return data.Download, nil
}

func (r *RealDebrid) GetCheckCached() bool {
	return r.CheckCached
}

func (r *RealDebrid) getTorrents(offset int, limit int) (int, []*types.Torrent, error) {
	url := fmt.Sprintf("%s/torrents?limit=%d", r.Host, limit)
	torrents := make([]*types.Torrent, 0)
	if offset > 0 {
		url = fmt.Sprintf("%s&offset=%d", url, offset)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.Do(req)

	if err != nil {
		return 0, torrents, err
	}

	if resp.StatusCode == http.StatusNoContent {
		return 0, torrents, nil
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return 0, torrents, fmt.Errorf("realdebrid API error: %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, torrents, err
	}
	totalItems, _ := strconv.Atoi(resp.Header.Get("X-Total-Count"))
	var data []TorrentsResponse
	if err = json.Unmarshal(body, &data); err != nil {
		return 0, nil, err
	}
	filenames := map[string]bool{}
	for _, t := range data {
		if t.Status != "downloaded" {
			continue
		}
		if _, exists := filenames[t.Filename]; exists {
			continue
		}
		torrents = append(torrents, &types.Torrent{
			Id:               t.Id,
			Name:             t.Filename,
			Bytes:            t.Bytes,
			Progress:         t.Progress,
			Status:           t.Status,
			Filename:         t.Filename,
			OriginalFilename: t.Filename,
			Links:            t.Links,
			Files:            make(map[string]types.File),
			InfoHash:         t.Hash,
			Debrid:           r.Name,
			MountPath:        r.MountPath,
			Added:            t.Added.Format(time.RFC3339),
		})
	}
	return totalItems, torrents, nil
}

func (r *RealDebrid) GetTorrents() ([]*types.Torrent, error) {
	limit := 5000

	// Get first batch and total count
	totalItems, firstBatch, err := r.getTorrents(0, limit)
	if err != nil {
		return nil, err
	}

	allTorrents := firstBatch

	// Calculate remaining requests
	remaining := totalItems - len(firstBatch)
	if remaining <= 0 {
		return allTorrents, nil
	}

	// Prepare for concurrent fetching
	var fetchError error

	// Calculate how many more requests we need
	batchCount := (remaining + limit - 1) / limit // ceiling division

	for i := 1; i <= batchCount; i++ {
		_, batch, err := r.getTorrents(i*limit, limit)
		if err != nil {
			fetchError = err
			continue
		}
		allTorrents = append(allTorrents, batch...)
	}

	if fetchError != nil {
		return nil, fetchError
	}

	return allTorrents, nil
}

func (r *RealDebrid) GetDownloads() (map[string]types.DownloadLinks, error) {
	links := make(map[string]types.DownloadLinks)
	offset := 0
	limit := 5000
	for {
		dl, err := r._getDownloads(offset, limit)
		if err != nil {
			break
		}
		if len(dl) == 0 {
			break
		}

		for _, d := range dl {
			if _, exists := links[d.Link]; exists {
				// This is ordered by date, so we can skip the rest
				continue
			}
			links[d.Link] = d
		}

		offset += len(dl)
	}
	return links, nil
}

func (r *RealDebrid) _getDownloads(offset int, limit int) ([]types.DownloadLinks, error) {
	url := fmt.Sprintf("%s/downloads?limit=%d", r.Host, limit)
	if offset > 0 {
		url = fmt.Sprintf("%s&offset=%d", url, offset)
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data []DownloadsResponse
	if err = json.Unmarshal(resp, &data); err != nil {
		return nil, err
	}
	links := make([]types.DownloadLinks, 0)
	for _, d := range data {
		links = append(links, types.DownloadLinks{
			Filename:     d.Filename,
			Size:         d.Filesize,
			Link:         d.Link,
			DownloadLink: d.Download,
			Generated:    d.Generated,
			Id:           d.Id,
		})

	}
	return links, nil
}

func (r *RealDebrid) GetDownloadingStatus() []string {
	return []string{"downloading", "magnet_conversion", "queued", "compressing", "uploading"}
}

func (r *RealDebrid) GetDownloadUncached() bool {
	return r.DownloadUncached
}

func (r *RealDebrid) GetMountPath() string {
	return r.MountPath
}

func New(dc config.Debrid) *RealDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	_log := logger.NewLogger(dc.Name)
	client := request.New().
		WithHeaders(headers).
		WithRateLimiter(rl).WithLogger(_log).
		WithMaxRetries(5).
		WithRetryableStatus(429)
	return &RealDebrid{
		Name:             "realdebrid",
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		MountPath:        dc.Folder,
		logger:           logger.NewLogger(dc.Name),
		CheckCached:      dc.CheckCached,
	}
}
