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
	"strings"
)

type RealDebrid struct {
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *common.RLHTTPClient
	cache            *common.Cache
	MountPath        string
	logger           *log.Logger
}

func (r *RealDebrid) GetMountPath() string {
	return r.MountPath
}

func (r *RealDebrid) GetName() string {
	return "realdebrid"
}

func (r *RealDebrid) GetLogger() *log.Logger {
	return r.logger
}

func GetTorrentFiles(data structs.RealDebridTorrentInfo) []TorrentFile {
	files := make([]TorrentFile, 0)
	for _, f := range data.Files {
		name := filepath.Base(f.Path)
		if (!common.RegexMatch(common.VIDEOMATCH, name) && !common.RegexMatch(common.SUBMATCH, name)) || common.RegexMatch(common.SAMPLEMATCH, name) {
			continue
		}
		fileId := f.ID
		file := &TorrentFile{
			Name: name,
			Path: name,
			Size: int64(f.Bytes),
			Id:   strconv.Itoa(fileId),
		}
		files = append(files, *file)
	}
	return files
}

func (r *RealDebrid) IsAvailable(infohashes []string) map[string]bool {
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

		hashStr := strings.Join(validHashes, "/")
		url := fmt.Sprintf("%s/torrents/instantAvailability/%s", r.Host, hashStr)
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

func (r *RealDebrid) GetTorrent(id string) (*Torrent, error) {
	torrent := &Torrent{}
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, id)
	resp, err := r.client.MakeRequest(http.MethodGet, url, nil)
	if err != nil {
		return torrent, err
	}
	var data structs.RealDebridTorrentInfo
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return torrent, err
	}
	name := common.RemoveInvalidChars(data.OriginalFilename)
	torrent.Id = id
	torrent.Name = name
	torrent.Bytes = data.Bytes
	torrent.Folder = name
	torrent.Progress = data.Progress
	torrent.Status = data.Status
	torrent.Speed = data.Speed
	torrent.Seeders = data.Seeders
	torrent.Filename = data.Filename
	torrent.OriginalFilename = data.OriginalFilename
	files := GetTorrentFiles(data)
	torrent.Files = files
	return torrent, nil
}

func (r *RealDebrid) CheckStatus(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, torrent.Id)
	for {
		resp, err := r.client.MakeRequest(http.MethodGet, url, nil)
		if err != nil {
			log.Println("ERROR Checking file: ", err)
			return torrent, err
		}
		var data structs.RealDebridTorrentInfo
		err = json.Unmarshal(resp, &data)
		status := data.Status
		name := common.RemoveInvalidChars(data.OriginalFilename)
		torrent.Name = name // Important because some magnet changes the name
		torrent.Folder = name
		torrent.Filename = data.Filename
		torrent.OriginalFilename = data.OriginalFilename
		torrent.Bytes = data.Bytes
		torrent.Progress = data.Progress
		torrent.Speed = data.Speed
		torrent.Seeders = data.Seeders
		if status == "error" || status == "dead" || status == "magnet_error" {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		} else if status == "waiting_files_selection" {
			files := GetTorrentFiles(data)
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
			if !r.DownloadUncached {
				// @TODO: Delete the torrent if it's not cached
				return torrent, fmt.Errorf("torrent: %s not cached", torrent.Name)
			}
		}

	}
	return torrent, nil
}

func (r *RealDebrid) DownloadLink(torrent *Torrent) error {
	return nil
}

func (r *RealDebrid) GetDownloadUncached() bool {
	return r.DownloadUncached
}

func NewRealDebrid(dc common.DebridConfig, cache *common.Cache) *RealDebrid {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	logger := common.NewLogger(dc.Name, os.Stdout)
	return &RealDebrid{
		Host:             dc.Host,
		APIKey:           dc.APIKey,
		DownloadUncached: dc.DownloadUncached,
		client:           client,
		cache:            cache,
		MountPath:        dc.Folder,
		logger:           logger,
	}
}
