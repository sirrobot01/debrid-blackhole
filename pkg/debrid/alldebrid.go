package debrid

import (
	"encoding/json"
	"fmt"
	"goBlack/common"
	"goBlack/pkg/debrid/structs"
	"log"
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

func (r *AllDebrid) GetLogger() *log.Logger {
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
	var data structs.AllDebridUploadMagnetResponse
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
	r.logger.Printf("Torrent: %s added with id: %s\n", torrent.Name, torrentId)
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

func (r *AllDebrid) GetTorrent(id string) (*Torrent, error) {
	torrent := &Torrent{}
	url := fmt.Sprintf("%s/magnet/status?id=%s", r.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return torrent, err
	}
	var res structs.AllDebridTorrentInfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		r.logger.Printf("Error unmarshalling torrent info: %s\n", err)
		return torrent, err
	}
	data := res.Data.Magnets
	name := data.Filename
	torrent.Id = id
	torrent.Name = name
	torrent.Bytes = data.Size
	torrent.Folder = name
	torrent.Progress = float64((data.Downloaded / data.Size) * 100)
	torrent.Status = getAlldebridStatus(data.StatusCode)
	torrent.Speed = data.DownloadSpeed
	torrent.Seeders = data.Seeders
	torrent.Filename = name
	torrent.OriginalFilename = name
	files := make([]TorrentFile, 0)
	for index, f := range data.Files {
		fileName := filepath.Base(f.Name)
		if common.RegexMatch(common.SAMPLEMATCH, fileName) {
			// Skip sample files
			continue
		}
		if !common.RegexMatch(common.VIDEOMATCH, fileName) && !common.RegexMatch(common.MUSICMATCH, fileName) {
			continue
		}
		file := TorrentFile{
			Id:   strconv.Itoa(index),
			Name: fileName,
			Size: f.Size,
			Path: fileName,
		}
		files = append(files, file)
	}
	parentFolder := data.Filename

	if data.NbLinks == 1 {
		// All debrid doesn't return the parent folder for single file torrents
		parentFolder = ""
	}
	torrent.OriginalFilename = parentFolder
	torrent.Files = files
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
			r.logger.Printf("Torrent: %s downloaded\n", torrent.Name)
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
		r.logger.Printf("Torrent: %s deleted\n", torrent.Name)
	} else {
		r.logger.Printf("Error deleting torrent: %s", err)
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
		var data structs.AllDebridDownloadLink
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

func NewAllDebrid(dc common.DebridConfig, cache *common.Cache) *AllDebrid {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	logger := common.NewLogger(dc.Name, os.Stdout)
	return &AllDebrid{
		BaseDebrid: BaseDebrid{
			Name:             "alldebrid",
			Host:             dc.Host,
			APIKey:           dc.APIKey,
			DownloadUncached: dc.DownloadUncached,
			client:           client,
			cache:            cache,
			MountPath:        dc.Folder,
			logger:           logger,
			CheckCached:      dc.CheckCached,
		},
	}
}
