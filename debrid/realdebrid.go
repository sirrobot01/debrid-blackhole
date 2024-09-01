package debrid

import (
	"encoding/json"
	"fmt"
	"goBlack/common"
	"goBlack/debrid/structs"
	"log"
	"net/http"
	gourl "net/url"
	"path/filepath"
	"strconv"
	"strings"
)

type RealDebrid struct {
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *common.RLHTTPClient
	cache            *common.Cache
}

func (r *RealDebrid) Process(arr *Arr, magnet string) (*Torrent, error) {
	torrent, err := GetTorrentInfo(magnet)
	torrent.Arr = arr
	if err != nil {
		return torrent, err
	}
	log.Printf("Torrent Name: %s", torrent.Name)
	if !r.DownloadUncached {
		hash, exists := r.IsAvailable([]string{torrent.InfoHash})[torrent.InfoHash]
		if !exists || !hash {
			return torrent, fmt.Errorf("torrent is not cached")
		}
		log.Printf("Torrent: %s is cached", torrent.Name)
	}

	torrent, err = r.SubmitMagnet(torrent)
	if err != nil || torrent.Id == "" {
		return nil, err
	}
	return r.CheckStatus(torrent)
}

func (r *RealDebrid) IsAvailable(infohashes []string) map[string]bool {
	// Check if the infohashes are available in the local cache
	hashes, result := GetLocalCache(infohashes, r.cache)

	if hashes == "" {
		// Either all the infohashes are locally cached or none are
		r.cache.AddMultiple(result)
		return result
	}

	url := fmt.Sprintf("%s/torrents/instantAvailability/%s", r.Host, hashes)
	resp, err := r.client.MakeRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Println("Error checking availability:", err)
		return result
	}
	var data structs.RealDebridAvailabilityResponse
	err = json.Unmarshal(resp, &data)
	if err != nil {
		log.Println("Error marshalling availability:", err)
		return result
	}
	for _, h := range infohashes {
		hosters, exists := data[strings.ToLower(h)]
		if exists && len(hosters.Rd) > 0 {
			result[h] = true
		}
	}
	r.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (r *RealDebrid) SubmitMagnet(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/torrents/addMagnet", r.Host)
	payload := gourl.Values{
		"magnet": {torrent.Magnet.Link},
	}
	var data structs.RealDebridAddMagnetSchema
	resp, err := r.client.MakeRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(resp, &data)
	log.Printf("Torrent: %s added with id: %s\n", torrent.Name, data.Id)
	torrent.Id = data.Id

	return torrent, nil
}

func (r *RealDebrid) CheckStatus(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, torrent.Id)
	for {
		resp, err := r.client.MakeRequest(http.MethodGet, url, nil)
		if err != nil {
			return torrent, err
		}
		var data structs.RealDebridTorrentInfo
		err = json.Unmarshal(resp, &data)
		status := data.Status
		torrent.Folder = common.RemoveExtension(data.OriginalFilename)
		if status == "error" || status == "dead" || status == "magnet_error" {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		} else if status == "waiting_files_selection" {
			files := make([]TorrentFile, 0)
			for _, f := range data.Files {
				name := f.Path
				if !common.RegexMatch(common.VIDEOMATCH, name) && !common.RegexMatch(common.SUBMATCH, name) {
					continue
				}
				fileId := f.ID
				file := &TorrentFile{
					Name: name,
					Path: filepath.Join(torrent.Folder, name),
					Size: int64(f.Bytes),
					Id:   strconv.Itoa(fileId),
				}
				files = append(files, *file)
			}
			torrent.Files = files
			if len(files) == 0 {
				return torrent, fmt.Errorf("no video files found")
			}
			filesId := make([]string, 0)
			for _, f := range files {
				filesId = append(filesId, f.Id)
			}
			p := gourl.Values{
				"files": {strings.Join(filesId, ",")},
			}
			payload := strings.NewReader(p.Encode())
			_, err = r.client.MakeRequest(http.MethodPost, fmt.Sprintf("%s/torrents/selectFiles/%s", r.Host, torrent.Id), payload)
			if err != nil {
				return torrent, err
			}
		} else if status == "downloaded" {
			log.Printf("Torrent: %s downloaded\n", torrent.Name)
			err = r.DownloadLink(torrent)
			if err != nil {
				return torrent, err
			}
			break
		} else if status == "downloading" {
			return torrent, fmt.Errorf("torrent is uncached")
		}

	}
	return torrent, nil
}

func (r *RealDebrid) DownloadLink(torrent *Torrent) error {
	return nil
}

func NewRealDebrid(dc common.DebridConfig, cache *common.Cache) *RealDebrid {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	return &RealDebrid{
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		cache:            cache,
	}
}
