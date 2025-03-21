package debrid_link

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
	"slices"
	"time"

	"net/http"
	"strings"
)

type DebridLink struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *request.Client

	MountPath   string
	logger      zerolog.Logger
	CheckCached bool
}

func (dl *DebridLink) GetName() string {
	return dl.Name
}

func (dl *DebridLink) GetLogger() zerolog.Logger {
	return dl.logger
}

func (dl *DebridLink) IsAvailable(hashes []string) map[string]bool {
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
	return result
}

func (dl *DebridLink) UpdateTorrent(t *types.Torrent) error {
	url := fmt.Sprintf("%s/seedbox/list?ids=%s", dl.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := dl.client.MakeRequest(req)
	if err != nil {
		return err
	}
	var res TorrentInfo
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("error getting torrent")
	}
	if res.Value == nil {
		return fmt.Errorf("torrent not found")
	}
	dt := *res.Value

	if len(dt) == 0 {
		return fmt.Errorf("torrent not found")
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
	cfg := config.GetConfig()
	for _, f := range data.Files {
		if !cfg.IsSizeAllowed(f.Size) {
			continue
		}
		file := types.File{
			Id:           f.ID,
			Name:         f.Name,
			Size:         f.Size,
			Path:         f.Name,
			DownloadLink: f.DownloadURL,
			Link:         f.DownloadURL,
		}
		t.Files[f.Name] = file
	}
	return nil
}

func (dl *DebridLink) SubmitMagnet(t *types.Torrent) (*types.Torrent, error) {
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
	for _, f := range data.Files {
		file := types.File{
			Id:           f.ID,
			Name:         f.Name,
			Size:         f.Size,
			Path:         f.Name,
			Link:         f.DownloadURL,
			DownloadLink: f.DownloadURL,
			Generated:    time.Now(),
		}
		t.Files[f.Name] = file
	}

	return t, nil
}

func (dl *DebridLink) CheckStatus(torrent *types.Torrent, isSymlink bool) (*types.Torrent, error) {
	for {
		err := dl.UpdateTorrent(torrent)
		if err != nil || torrent == nil {
			return torrent, err
		}
		status := torrent.Status
		if status == "downloaded" {
			dl.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			err = dl.GenerateDownloadLinks(torrent)
			if err != nil {
				return torrent, err
			}
			break
		} else if slices.Contains(dl.GetDownloadingStatus(), status) {
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

func (dl *DebridLink) DeleteTorrent(torrentId string) {
	url := fmt.Sprintf("%s/seedbox/%s/remove", dl.Host, torrentId)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := dl.client.MakeRequest(req)
	if err == nil {
		dl.logger.Info().Msgf("Torrent: %s deleted", torrentId)
	} else {
		dl.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (dl *DebridLink) GenerateDownloadLinks(t *types.Torrent) error {
	return nil
}

func (dl *DebridLink) GetDownloads() (map[string]types.DownloadLinks, error) {
	return nil, nil
}

func (dl *DebridLink) GetDownloadLink(t *types.Torrent, file *types.File) *types.File {
	return file
}

func (dl *DebridLink) GetDownloadingStatus() []string {
	return []string{"downloading"}
}

func (dl *DebridLink) GetCheckCached() bool {
	return dl.CheckCached
}

func (dl *DebridLink) GetDownloadUncached() bool {
	return dl.DownloadUncached
}

func New(dc config.Debrid) *DebridLink {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
		"Content-Type":  "application/json",
	}
	_log := logger.NewLogger(dc.Name)
	client := request.New().
		WithHeaders(headers).
		WithRateLimiter(rl).WithLogger(_log)
	return &DebridLink{
		Name:             "debridlink",
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		MountPath:        dc.Folder,
		logger:           logger.NewLogger(dc.Name),
		CheckCached:      dc.CheckCached,
	}
}

func (dl *DebridLink) GetTorrents() ([]*types.Torrent, error) {
	return nil, nil
}

func (dl *DebridLink) ConvertLinksToFiles(links []string) []types.File {
	return nil
}
