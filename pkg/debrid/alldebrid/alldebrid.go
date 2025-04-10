package alldebrid

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"net/http"
	gourl "net/url"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

type AllDebrid struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadKeys     *xsync.MapOf[string, types.Account]
	DownloadUncached bool
	client           *request.Client

	MountPath   string
	logger      zerolog.Logger
	CheckCached bool
}

func New(dc config.Debrid) *AllDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)

	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	_log := logger.New(dc.Name)
	client := request.New(
		request.WithHeaders(headers),
		request.WithLogger(_log),
		request.WithRateLimiter(rl),
		request.WithProxy(dc.Proxy),
	)

	accounts := xsync.NewMapOf[string, types.Account]()
	for idx, key := range dc.DownloadAPIKeys {
		id := strconv.Itoa(idx)
		accounts.Store(id, types.Account{
			Name:  key,
			ID:    id,
			Token: key,
		})
	}
	return &AllDebrid{
		Name:             "alldebrid",
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadKeys:     accounts,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		MountPath:        dc.Folder,
		logger:           logger.New(dc.Name),
		CheckCached:      dc.CheckCached,
	}
}

func (ad *AllDebrid) GetName() string {
	return ad.Name
}

func (ad *AllDebrid) GetLogger() zerolog.Logger {
	return ad.logger
}

func (ad *AllDebrid) IsAvailable(hashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	result := make(map[string]bool)

	// Divide hashes into groups of 100
	// AllDebrid does not support checking cached infohashes
	return result
}

func (ad *AllDebrid) SubmitMagnet(torrent *types.Torrent) (*types.Torrent, error) {
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

func flattenFiles(files []MagnetFile, parentPath string, index *int) map[string]types.File {
	result := make(map[string]types.File)

	cfg := config.Get()

	for _, f := range files {
		currentPath := f.Name
		if parentPath != "" {
			currentPath = filepath.Join(parentPath, f.Name)
		}

		if f.Elements != nil {
			// This is a folder, recurse into it
			subFiles := flattenFiles(f.Elements, currentPath, index)
			for k, v := range subFiles {
				if _, ok := result[k]; ok {
					// File already exists, use path as key
					result[v.Path] = v
				} else {
					result[k] = v
				}
			}
		} else {
			// This is a file
			fileName := filepath.Base(f.Name)

			// Skip sample files
			if utils.IsSampleFile(f.Name) {
				continue
			}
			if !cfg.IsAllowedFile(fileName) {
				continue
			}

			if !cfg.IsSizeAllowed(f.Size) {
				continue
			}

			*index++
			file := types.File{
				Id:   strconv.Itoa(*index),
				Name: fileName,
				Size: f.Size,
				Path: currentPath,
				Link: f.Link,
			}
			result[file.Name] = file
		}
	}

	return result
}

func (ad *AllDebrid) UpdateTorrent(t *types.Torrent) error {
	url := fmt.Sprintf("%s/magnet/status?id=%s", ad.Host, t.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	if err != nil {
		return err
	}
	var res TorrentInfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		ad.logger.Info().Msgf("Error unmarshalling torrent info: %s", err)
		return err
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
	if status == "downloaded" {
		t.Bytes = data.Size

		t.Progress = float64((data.Downloaded / data.Size) * 100)
		t.Speed = data.DownloadSpeed
		t.Seeders = data.Seeders
		index := -1
		files := flattenFiles(data.Files, "", &index)
		t.Files = files
	}
	return nil
}

func (ad *AllDebrid) CheckStatus(torrent *types.Torrent, isSymlink bool) (*types.Torrent, error) {
	for {
		err := ad.UpdateTorrent(torrent)

		if err != nil || torrent == nil {
			return torrent, err
		}
		status := torrent.Status
		if status == "downloaded" {
			ad.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			if !isSymlink {
				err = ad.GenerateDownloadLinks(torrent)
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
			return torrent, nil
		} else {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		}

	}
	return torrent, nil
}

func (ad *AllDebrid) DeleteTorrent(torrentId string) error {
	url := fmt.Sprintf("%s/magnet/delete?id=%s", ad.Host, torrentId)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if _, err := ad.client.MakeRequest(req); err != nil {
		return err
	}
	ad.logger.Info().Msgf("Torrent %s deleted from AD", torrentId)
	return nil
}

func (ad *AllDebrid) GenerateDownloadLinks(t *types.Torrent) error {
	filesCh := make(chan types.File, len(t.Files))
	errCh := make(chan error, len(t.Files))

	var wg sync.WaitGroup
	wg.Add(len(t.Files))
	for _, file := range t.Files {
		go func(file types.File) {
			defer wg.Done()
			link, accountId, err := ad.GetDownloadLink(t, &file)
			if err != nil {
				errCh <- err
				return
			}
			file.DownloadLink = link
			file.Generated = time.Now()
			file.AccountId = accountId
			if link == "" {
				errCh <- fmt.Errorf("error getting download links %w", err)
				return
			}
			filesCh <- file
		}(file)
	}
	go func() {
		wg.Wait()
		close(filesCh)
		close(errCh)
	}()
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

func (ad *AllDebrid) GetDownloadLink(t *types.Torrent, file *types.File) (string, string, error) {
	url := fmt.Sprintf("%s/link/unlock", ad.Host)
	query := gourl.Values{}
	query.Add("link", file.Link)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	if err != nil {
		return "", "", err
	}
	var data DownloadLink
	if err = json.Unmarshal(resp, &data); err != nil {
		return "", "", err
	}
	link := data.Data.Link
	if link == "" {
		return "", "", fmt.Errorf("error getting download links %s", data.Error.Message)
	}
	return link, "0", nil
}

func (ad *AllDebrid) GetCheckCached() bool {
	return ad.CheckCached
}

func (ad *AllDebrid) GetTorrents() ([]*types.Torrent, error) {
	url := fmt.Sprintf("%s/magnet/status?status=ready", ad.Host)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := ad.client.MakeRequest(req)
	torrents := make([]*types.Torrent, 0)
	if err != nil {
		return torrents, err
	}
	var res TorrentsListResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		ad.logger.Info().Msgf("Error unmarshalling torrent info: %s", err)
		return torrents, err
	}
	for _, magnet := range res.Data.Magnets {
		torrents = append(torrents, &types.Torrent{
			Id:               strconv.Itoa(magnet.Id),
			Name:             magnet.Filename,
			Bytes:            magnet.Size,
			Status:           getAlldebridStatus(magnet.StatusCode),
			Filename:         magnet.Filename,
			OriginalFilename: magnet.Filename,
			Files:            make(map[string]types.File),
			InfoHash:         magnet.Hash,
			Debrid:           ad.Name,
			MountPath:        ad.MountPath,
		})
	}

	return torrents, nil
}

func (ad *AllDebrid) GetDownloads() (map[string]types.DownloadLinks, error) {
	return nil, nil
}

func (ad *AllDebrid) GetDownloadingStatus() []string {
	return []string{"downloading"}
}

func (ad *AllDebrid) GetDownloadUncached() bool {
	return ad.DownloadUncached
}

func (ad *AllDebrid) CheckLink(link string) error {
	return nil
}

func (ad *AllDebrid) GetMountPath() string {
	return ad.MountPath
}

func (ad *AllDebrid) DisableAccount(accountId string) {
}

func (ad *AllDebrid) ResetActiveDownloadKeys() {

}
