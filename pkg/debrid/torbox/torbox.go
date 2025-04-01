package torbox

import (
	"bytes"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"mime/multipart"
	"net/http"
	gourl "net/url"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Torbox struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	ExtraAPIKeys     []string
	DownloadUncached bool
	client           *request.Client

	MountPath   string
	logger      zerolog.Logger
	CheckCached bool
}

func New(dc config.Debrid) *Torbox {
	rl := request.ParseRateLimit(dc.RateLimit)
	apiKeys := strings.Split(dc.APIKey, ",")
	extraKeys := make([]string, 0)
	if len(apiKeys) > 1 {
		extraKeys = apiKeys[1:]
	}
	mainKey := apiKeys[0]
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", mainKey),
	}
	_log := logger.New(dc.Name)
	client := request.New(
		request.WithHeaders(headers),
		request.WithRateLimiter(rl),
		request.WithLogger(_log),
	)

	return &Torbox{
		Name:             "torbox",
		Host:             dc.Host,
		APIKey:           mainKey,
		ExtraAPIKeys:     extraKeys,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		MountPath:        dc.Folder,
		logger:           _log,
		CheckCached:      dc.CheckCached,
	}
}

func (tb *Torbox) GetName() string {
	return tb.Name
}

func (tb *Torbox) GetLogger() zerolog.Logger {
	return tb.logger
}

func (tb *Torbox) IsAvailable(hashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	result := make(map[string]bool)

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

		for h, c := range *res.Data {
			if c.Size > 0 {
				result[strings.ToUpper(h)] = true
			}
		}
	}
	return result
}

func (tb *Torbox) SubmitMagnet(torrent *types.Torrent) (*types.Torrent, error) {
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

func (tb *Torbox) UpdateTorrent(t *types.Torrent) error {
	url := fmt.Sprintf("%s/api/torrents/mylist/?id=%s", tb.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := tb.client.MakeRequest(req)
	if err != nil {
		return err
	}
	var res InfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return err
	}
	data := res.Data
	name := data.Name
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
	cfg := config.Get()
	for _, f := range data.Files {
		fileName := filepath.Base(f.Name)
		if utils.IsSampleFile(f.AbsolutePath) {
			// Skip sample files
			continue
		}
		if !cfg.IsAllowedFile(fileName) {
			continue
		}

		if !cfg.IsSizeAllowed(f.Size) {
			continue
		}
		file := types.File{
			Id:   strconv.Itoa(f.Id),
			Name: fileName,
			Size: f.Size,
			Path: fileName,
		}
		t.Files[fileName] = file
	}
	var cleanPath string
	if len(t.Files) > 0 {
		cleanPath = path.Clean(data.Files[0].Name)
	} else {
		cleanPath = path.Clean(data.Name)
	}

	t.OriginalFilename = strings.Split(cleanPath, "/")[0]
	t.Debrid = tb.Name
	return nil
}

func (tb *Torbox) CheckStatus(torrent *types.Torrent, isSymlink bool) (*types.Torrent, error) {
	for {
		err := tb.UpdateTorrent(torrent)

		if err != nil || torrent == nil {
			return torrent, err
		}
		status := torrent.Status
		if status == "downloaded" {
			tb.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			if !isSymlink {
				err = tb.GenerateDownloadLinks(torrent)
				if err != nil {
					return torrent, err
				}
			}
			break
		} else if slices.Contains(tb.GetDownloadingStatus(), status) {
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

func (tb *Torbox) DeleteTorrent(torrentId string) error {
	url := fmt.Sprintf("%s/api/torrents/controltorrent/%s", tb.Host, torrentId)
	payload := map[string]string{"torrent_id": torrentId, "action": "Delete"}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodDelete, url, bytes.NewBuffer(jsonPayload))
	if _, err := tb.client.MakeRequest(req); err != nil {
		return err
	}
	tb.logger.Info().Msgf("Torrent %s deleted from Torbox", torrentId)
	return nil
}

func (tb *Torbox) GenerateDownloadLinks(t *types.Torrent) error {
	filesCh := make(chan types.File, len(t.Files))
	errCh := make(chan error, len(t.Files))

	var wg sync.WaitGroup
	wg.Add(len(t.Files))
	for _, file := range t.Files {
		go func() {
			defer wg.Done()
			link, err := tb.GetDownloadLink(t, &file)
			if err != nil {
				errCh <- err
				return
			}
			file.DownloadLink = link
			filesCh <- file
		}()
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

func (tb *Torbox) GetDownloadLink(t *types.Torrent, file *types.File) (string, error) {
	url := fmt.Sprintf("%s/api/torrents/requestdl/", tb.Host)
	query := gourl.Values{}
	query.Add("torrent_id", t.Id)
	query.Add("token", tb.APIKey)
	query.Add("file_id", file.Id)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := tb.client.MakeRequest(req)
	if err != nil {
		return "", err
	}
	var data DownloadLinksResponse
	if err = json.Unmarshal(resp, &data); err != nil {
		return "", err
	}
	if data.Data == nil {
		return "", fmt.Errorf("error getting download links")
	}
	link := *data.Data
	return link, nil
}

func (tb *Torbox) GetDownloadingStatus() []string {
	return []string{"downloading"}
}

func (tb *Torbox) GetCheckCached() bool {
	return tb.CheckCached
}

func (tb *Torbox) GetTorrents() ([]*types.Torrent, error) {
	return nil, nil
}

func (tb *Torbox) GetDownloadUncached() bool {
	return tb.DownloadUncached
}

func (tb *Torbox) GetDownloads() (map[string]types.DownloadLinks, error) {
	return nil, nil
}

func (tb *Torbox) CheckLink(link string) error {
	return nil
}

func (tb *Torbox) GetMountPath() string {
	return tb.MountPath
}
