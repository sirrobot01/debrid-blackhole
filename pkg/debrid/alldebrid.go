package debrid

import (
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"net/http"
	gourl "net/url"
	"os"
	"path/filepath"
	"strconv"
)

type AllDebrid struct {
	BaseDebrid
}

func (r *AllDebrid) GetMountPath() string {
	return r.MountPath
}

func (r *AllDebrid) GetName() string {
	return r.Name
}

func (r *AllDebrid) GetLogger() zerolog.Logger {
	return r.logger
}

func (r *AllDebrid) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := GetLocalCache(infohashes, r.cache)

	if len(hashes) == 0 {
		// Either all the infohashes are locally cached or none are
		r.cache.AddMultiple(result)
		return result
	}

	// Divide hashes into groups of 100
	// AllDebrid does not support checking cached infohashes
	return result
}

func (r *AllDebrid) SubmitMagnet(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/magnet/upload", r.Host)
	query := gourl.Values{}
	query.Add("magnets[]", torrent.Magnet.Link)
	url += "?" + query.Encode()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data types.AllDebridUploadMagnetResponse
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
	r.logger.Info().Msgf("Torrent: %s added with id: %s", torrent.Name, torrentId)
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

func flattenFiles(files []types.AllDebridMagnetFile, parentPath string, index *int) []TorrentFile {
	result := make([]TorrentFile, 0)

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
			if common.RegexMatch(common.SAMPLEMATCH, fileName) {
				continue
			}
			if !cfg.IsAllowedFile(fileName) {
				continue
			}

			if !cfg.IsSizeAllowed(f.Size) {
				continue
			}

			*index++
			file := TorrentFile{
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

func (r *AllDebrid) GetTorrent(id string) (*Torrent, error) {
	torrent := &Torrent{}
	url := fmt.Sprintf("%s/magnet/status?id=%s", r.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return torrent, err
	}
	var res types.AllDebridTorrentInfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		r.logger.Info().Msgf("Error unmarshalling torrent info: %s", err)
		return torrent, err
	}
	data := res.Data.Magnets
	status := getAlldebridStatus(data.StatusCode)
	name := data.Filename
	torrent.Id = id
	torrent.Name = name
	torrent.Status = status
	torrent.Filename = name
	torrent.OriginalFilename = name
	torrent.Folder = name
	if status == "downloaded" {
		torrent.Bytes = data.Size

		torrent.Progress = float64((data.Downloaded / data.Size) * 100)
		torrent.Speed = data.DownloadSpeed
		torrent.Seeders = data.Seeders
		index := -1
		files := flattenFiles(data.Files, "", &index)
		parentFolder := data.Filename
		if data.NbLinks == 1 {
			// All debrid doesn't return the parent folder for single file torrents
			parentFolder = ""
		}
		torrent.OriginalFilename = parentFolder
		torrent.Files = files
	}
	torrent.Debrid = r
	return torrent, nil
}

func (r *AllDebrid) CheckStatus(torrent *Torrent, isSymlink bool) (*Torrent, error) {
	for {
		tb, err := r.GetTorrent(torrent.Id)

		torrent = tb

		if err != nil || tb == nil {
			return tb, err
		}
		status := torrent.Status
		if status == "downloaded" {
			r.logger.Info().Msgf("Torrent: %s downloaded", torrent.Name)
			if !isSymlink {
				err = r.GetDownloadLinks(torrent)
				if err != nil {
					return torrent, err
				}
			}
			break
		} else if status == "downloading" {
			if !r.DownloadUncached {
				go torrent.Delete()
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

func (r *AllDebrid) DeleteTorrent(torrent *Torrent) {
	url := fmt.Sprintf("%s/magnet/delete?id=%s", r.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Info().Msgf("Torrent: %s deleted", torrent.Name)
	} else {
		r.logger.Info().Msgf("Error deleting torrent: %s", err)
	}
}

func (r *AllDebrid) GetDownloadLinks(torrent *Torrent) error {
	downloadLinks := make([]TorrentDownloadLinks, 0)
	for _, file := range torrent.Files {
		url := fmt.Sprintf("%s/link/unlock", r.Host)
		query := gourl.Values{}
		query.Add("link", file.Link)
		url += "?" + query.Encode()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data types.AllDebridDownloadLink
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		link := data.Data.Link

		dl := TorrentDownloadLinks{
			Link:         file.Link,
			Filename:     data.Data.Filename,
			DownloadLink: link,
		}
		downloadLinks = append(downloadLinks, dl)
	}
	torrent.DownloadLinks = downloadLinks
	return nil
}

func (r *AllDebrid) GetCheckCached() bool {
	return r.CheckCached
}

func NewAllDebrid(dc config.Debrid, cache *common.Cache) *AllDebrid {
	rl := request.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := request.NewRLHTTPClient(rl, headers)
	return &AllDebrid{
		BaseDebrid: BaseDebrid{
			Name:             "alldebrid",
			Host:             dc.Host,
			APIKey:           dc.APIKey,
			DownloadUncached: dc.DownloadUncached,
			client:           client,
			cache:            cache,
			MountPath:        dc.Folder,
			logger:           logger.NewLogger(dc.Name, config.GetConfig().LogLevel, os.Stdout),
			CheckCached:      dc.CheckCached,
		},
	}
}
