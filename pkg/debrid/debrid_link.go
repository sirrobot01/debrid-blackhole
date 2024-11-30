package debrid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"goBlack/common"
	"goBlack/pkg/debrid/structs"
	"log"
	"net/http"
	"os"
	"strings"
)

type DebridLink struct {
	BaseDebrid
}

func (r *DebridLink) GetMountPath() string {
	return r.MountPath
}

func (r *DebridLink) GetName() string {
	return r.Name
}

func (r *DebridLink) GetLogger() *log.Logger {
	return r.logger
}

func (r *DebridLink) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := GetLocalCache(infohashes, r.cache)

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

		hashStr := strings.Join(validHashes, ",")
		url := fmt.Sprintf("%s/seedbox/cached/%s", r.Host, hashStr)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			log.Println("Error checking availability:", err)
			return result
		}
		var data structs.DebridLinkAvailableResponse
		err = json.Unmarshal(resp, &data)
		if err != nil {
			log.Println("Error marshalling availability:", err)
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
	r.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (r *DebridLink) GetTorrent(id string) (*Torrent, error) {
	torrent := &Torrent{}
	url := fmt.Sprintf("%s/seedbox/list/?ids=%s", r.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return torrent, err
	}
	var res structs.DebridLinkTorrentInfo
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return torrent, err
	}
	if res.Success == false {
		return torrent, fmt.Errorf("error getting torrent")
	}
	if res.Value == nil {
		return torrent, fmt.Errorf("torrent not found")
	}
	dt := *res.Value

	if len(dt) == 0 {
		return torrent, fmt.Errorf("torrent not found")
	}
	data := dt[0]
	status := "downloading"
	name := common.RemoveInvalidChars(data.Name)
	torrent.Id = data.ID
	torrent.Name = name
	torrent.Bytes = data.TotalSize
	torrent.Folder = name
	torrent.Progress = data.DownloadPercent
	torrent.Status = status
	torrent.Speed = data.DownloadSpeed
	torrent.Seeders = data.PeersConnected
	torrent.Filename = name
	torrent.OriginalFilename = name
	files := make([]TorrentFile, len(data.Files))
	for i, f := range data.Files {
		files[i] = TorrentFile{
			Id:   f.ID,
			Name: f.Name,
			Size: f.Size,
		}
	}
	torrent.Files = files
	torrent.Debrid = r
	return torrent, nil
}

func (r *DebridLink) SubmitMagnet(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/seedbox/add", r.Host)
	payload := map[string]string{"url": torrent.Magnet.Link}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonPayload))
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var res structs.DebridLinkSubmitTorrentInfo
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return nil, err
	}
	if res.Success == false || res.Value == nil {
		return nil, fmt.Errorf("error adding torrent")
	}
	data := *res.Value
	status := "downloading"
	log.Printf("Torrent: %s added with id: %s\n", torrent.Name, data.ID)
	name := common.RemoveInvalidChars(data.Name)
	torrent.Id = data.ID
	torrent.Name = name
	torrent.Bytes = data.TotalSize
	torrent.Folder = name
	torrent.Progress = data.DownloadPercent
	torrent.Status = status
	torrent.Speed = data.DownloadSpeed
	torrent.Seeders = data.PeersConnected
	torrent.Filename = name
	torrent.OriginalFilename = name
	files := make([]TorrentFile, len(data.Files))
	for i, f := range data.Files {
		files[i] = TorrentFile{
			Id:   f.ID,
			Name: f.Name,
			Size: f.Size,
			Link: f.DownloadURL,
		}
	}
	torrent.Files = files
	torrent.Debrid = r

	return torrent, nil
}

func (r *DebridLink) CheckStatus(torrent *Torrent, isSymlink bool) (*Torrent, error) {
	for {
		torrent, err := r.GetTorrent(torrent.Id)

		if err != nil || torrent == nil {
			return torrent, err
		}
		status := torrent.Status
		if status == "error" || status == "dead" || status == "magnet_error" {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		} else if status == "downloaded" {
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
		}

	}
	return torrent, nil
}

func (r *DebridLink) DeleteTorrent(torrent *Torrent) {
	url := fmt.Sprintf("%s/seedbox/%s/remove", r.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Printf("Torrent: %s deleted\n", torrent.Name)
	} else {
		r.logger.Printf("Error deleting torrent: %s", err)
	}
}

func (r *DebridLink) GetDownloadLinks(torrent *Torrent) error {
	downloadLinks := make([]TorrentDownloadLinks, 0)
	for _, f := range torrent.Files {
		dl := TorrentDownloadLinks{
			Link:     f.Link,
			Filename: f.Name,
		}
		downloadLinks = append(downloadLinks, dl)
	}
	torrent.DownloadLinks = downloadLinks
	return nil
}

func (r *DebridLink) GetCheckCached() bool {
	return r.CheckCached
}

func NewDebridLink(dc common.DebridConfig, cache *common.Cache) *DebridLink {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	logger := common.NewLogger(dc.Name, os.Stdout)
	return &DebridLink{
		BaseDebrid: BaseDebrid{
			Name:             "debridlink",
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
