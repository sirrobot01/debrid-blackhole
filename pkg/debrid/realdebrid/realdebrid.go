package realdebrid

import (
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"io"
	"net/http"
	gourl "net/url"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RealDebrid struct {
	Name string
	Host string `json:"host"`

	APIKey       string
	DownloadKeys *xsync.MapOf[string, types.Account] // index | Account

	DownloadUncached bool
	client           *request.Client
	downloadClient   *request.Client

	MountPath   string
	logger      zerolog.Logger
	CheckCached bool
}

func New(dc config.Debrid) *RealDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	_log := logger.New(dc.Name)

	accounts := xsync.NewMapOf[string, types.Account]()
	firstDownloadKey := dc.DownloadAPIKeys[0]
	for idx, key := range dc.DownloadAPIKeys {
		id := strconv.Itoa(idx)
		accounts.Store(id, types.Account{
			Name:  key,
			ID:    id,
			Token: key,
		})
	}

	downloadHeaders := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", firstDownloadKey),
	}

	downloadClient := request.New(
		request.WithHeaders(downloadHeaders),
		request.WithRateLimiter(rl),
		request.WithLogger(_log),
		request.WithMaxRetries(5),
		request.WithRetryableStatus(429, 447),
		request.WithProxy(dc.Proxy),
		request.WithStatusCooldown(447, 10*time.Second), // 447 is a fair use error
	)

	client := request.New(
		request.WithHeaders(headers),
		request.WithRateLimiter(rl),
		request.WithLogger(_log),
		request.WithMaxRetries(5),
		request.WithRetryableStatus(429),
		request.WithProxy(dc.Proxy),
	)

	return &RealDebrid{
		Name:             "realdebrid",
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadKeys:     accounts,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		downloadClient:   downloadClient,
		MountPath:        dc.Folder,
		logger:           logger.New(dc.Name),
		CheckCached:      dc.CheckCached,
	}
}

func (r *RealDebrid) GetName() string {
	return r.Name
}

func (r *RealDebrid) GetLogger() zerolog.Logger {
	return r.logger
}

func getSelectedFiles(t *types.Torrent, data TorrentInfo) map[string]types.File {
	selectedFiles := make([]types.File, 0)
	for _, f := range data.Files {
		if f.Selected == 1 {
			name := filepath.Base(f.Path)
			file := types.File{
				Name: name,
				Path: name,
				Size: f.Bytes,
				Id:   strconv.Itoa(f.ID),
			}
			selectedFiles = append(selectedFiles, file)
		}
	}
	files := make(map[string]types.File)
	for index, f := range selectedFiles {
		if index >= len(data.Links) {
			break
		}
		f.Link = data.Links[index]
		files[f.Name] = f
	}
	return files
}

// getTorrentFiles returns a list of torrent files from the torrent info
// validate is used to determine if the files should be validated
// if validate is false, selected files will be returned
func getTorrentFiles(t *types.Torrent, data TorrentInfo) map[string]types.File {
	files := make(map[string]types.File)
	cfg := config.Get()
	idx := 0

	for _, f := range data.Files {
		name := filepath.Base(f.Path)
		if utils.IsSampleFile(f.Path) {
			// Skip sample files
			continue
		}

		if !cfg.IsAllowedFile(name) {
			continue
		}
		if !cfg.IsSizeAllowed(f.Bytes) {
			continue
		}

		file := types.File{
			Name: name,
			Path: name,
			Size: f.Bytes,
			Id:   strconv.Itoa(f.ID),
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
	t.Files = getSelectedFiles(t, data) // Get selected files
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
			t.Files = getTorrentFiles(t, data)
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
			t.Files = getSelectedFiles(t, data) // Get selected files
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
			return t, nil
		} else {
			return t, fmt.Errorf("torrent: %s has error: %s", t.Name, status)
		}

	}
	return t, nil
}

func (r *RealDebrid) DeleteTorrent(torrentId string) error {
	url := fmt.Sprintf("%s/torrents/delete/%s", r.Host, torrentId)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	if _, err := r.client.MakeRequest(req); err != nil {
		return err
	}
	r.logger.Info().Msgf("Torrent: %s deleted from RD", torrentId)
	return nil
}

func (r *RealDebrid) GenerateDownloadLinks(t *types.Torrent) error {
	filesCh := make(chan types.File, len(t.Files))
	errCh := make(chan error, len(t.Files))

	var wg sync.WaitGroup
	wg.Add(len(t.Files))
	for _, f := range t.Files {
		go func(file types.File) {
			defer wg.Done()

			link, accountId, err := r.GetDownloadLink(t, &file)
			if err != nil {
				errCh <- err
				return
			}

			file.DownloadLink = link
			file.AccountId = accountId
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
		return request.HosterUnavailableError // File has been removed
	}
	return nil
}

func (r *RealDebrid) _getDownloadLink(file *types.File) (string, error) {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	payload := gourl.Values{
		"link": {file.Link},
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Read the response body to get the error message
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		var data ErrorResponse
		if err = json.Unmarshal(b, &data); err != nil {
			return "", err
		}
		switch data.ErrorCode {
		case 23:
			return "", request.TrafficExceededError
		case 24:
			return "", request.HosterUnavailableError // Link has been nerfed
		case 19:
			return "", request.HosterUnavailableError // File has been removed
		case 36:
			return "", request.TrafficExceededError // traffic exceeded
		case 34:
			return "", request.TrafficExceededError // traffic exceeded
		default:
			return "", fmt.Errorf("realdebrid API error: Status: %d || Code: %d", resp.StatusCode, data.ErrorCode)
		}
	}
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

func (r *RealDebrid) GetDownloadLink(t *types.Torrent, file *types.File) (string, string, error) {
	defer r.downloadClient.SetHeader("Authorization", fmt.Sprintf("Bearer %s", r.APIKey))
	var (
		downloadLink string
		accountId    string
		err          error
	)
	accounts := r.getActiveAccounts()
	if len(accounts) < 1 {
		// No active download keys. It's likely that the key has reached bandwidth limit
		return "", "", fmt.Errorf("no active download keys")
	}
	for _, account := range accounts {
		r.downloadClient.SetHeader("Authorization", fmt.Sprintf("Bearer %s", account.Token))
		downloadLink, err = r._getDownloadLink(file)
		if err != nil {
			if errors.Is(err, request.TrafficExceededError) {
				continue
			}
			// If the error is not traffic exceeded, skip generating the link with a new key
			return "", "", err
		} else {
			// If we successfully generated a link, break the loop
			accountId = account.ID
			file.AccountId = accountId
			break
		}

	}
	if downloadLink != "" {
		// If we successfully generated a link, return it
		return downloadLink, accountId, nil
	}
	// If we reach here, it means all keys are disabled or traffic exceeded
	if err != nil {
		if errors.Is(err, request.TrafficExceededError) {
			return "", "", request.TrafficExceededError
		}
		return "", "", fmt.Errorf("error generating download link: %v", err)
	}
	return "", "", fmt.Errorf("error generating download link: %v", err)
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
	filenames := map[string]struct{}{}
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
		filenames[t.Filename] = struct{}{}
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
	limit := 1000

	accounts := r.getActiveAccounts()

	if len(accounts) < 1 {
		// No active download keys. It's likely that the key has reached bandwidth limit
		return nil, fmt.Errorf("no active download keys")
	}
	r.downloadClient.SetHeader("Authorization", fmt.Sprintf("Bearer %s", accounts[0].Token))
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
	resp, err := r.downloadClient.MakeRequest(req)
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

func (r *RealDebrid) DisableAccount(accountId string) {
	if value, ok := r.DownloadKeys.Load(accountId); ok {
		value.Disabled = true
		r.DownloadKeys.Store(accountId, value)
		r.logger.Info().Msgf("Disabled account Index: %s", value.ID)
	}
}

func (r *RealDebrid) ResetActiveDownloadKeys() {
	r.DownloadKeys.Range(func(key string, value types.Account) bool {
		value.Disabled = false
		r.DownloadKeys.Store(key, value)
		return true
	})
}

func (r *RealDebrid) getActiveAccounts() []types.Account {
	accounts := make([]types.Account, 0)
	r.DownloadKeys.Range(func(key string, value types.Account) bool {
		if value.Disabled {
			return true
		}
		accounts = append(accounts, value)
		return true
	})
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].ID < accounts[j].ID
	})
	return accounts
}
