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
	BaseDebrid
}

func (r *RealDebrid) GetMountPath() string {
	return r.MountPath
}

func (r *RealDebrid) GetName() string {
	return r.Name
}

func (r *RealDebrid) GetLogger() *log.Logger {
	return r.logger
}

func GetTorrentFiles(data structs.RealDebridTorrentInfo) []TorrentFile {
	files := make([]TorrentFile, 0)
	for _, f := range data.Files {
		name := filepath.Base(f.Path)
		if (!common.RegexMatch(common.VIDEOMATCH, name) &&
			!common.RegexMatch(common.SUBMATCH, name) &&
			!common.RegexMatch(common.MUSICMATCH, name)) || common.RegexMatch(common.SAMPLEMATCH, name) {
			continue
		}
		fileId := f.ID
		file := TorrentFile{
			Name: name,
			Path: name,
			Size: f.Bytes,
			Id:   strconv.Itoa(fileId),
		}
		files = append(files, file)
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
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
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
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
	resp, err := r.client.MakeRequest(req)
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
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
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
	torrent.Links = data.Links
	torrent.Debrid = r
	files := GetTorrentFiles(data)
	torrent.Files = files
	return torrent, nil
}

func (r *RealDebrid) CheckStatus(torrent *Torrent, isSymlink bool) (*Torrent, error) {
	url := fmt.Sprintf("%s/torrents/info/%s", r.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for {
		resp, err := r.client.MakeRequest(req)
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
		torrent.Links = data.Links
		torrent.Status = status
		torrent.Debrid = r
		if status == "error" || status == "dead" || status == "magnet_error" {
			return torrent, fmt.Errorf("torrent: %s has error", torrent.Name)
		} else if status == "waiting_files_selection" {
			files := GetTorrentFiles(data)
			torrent.Files = files
			if len(files) == 0 {
				go torrent.Delete()
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
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/torrents/selectFiles/%s", r.Host, torrent.Id), payload)
			_, err = r.client.MakeRequest(req)
			if err != nil {
				return torrent, err
			}
		} else if status == "downloaded" {
			files := GetTorrentFiles(data)
			torrent.Files = files
			log.Printf("Torrent: %s downloaded to RD\n", torrent.Name)
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

func (r *RealDebrid) DeleteTorrent(torrent *Torrent) {
	url := fmt.Sprintf("%s/torrents/delete/%s", r.Host, torrent.Id)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Printf("Torrent: %s deleted\n", torrent.Name)
	} else {
		r.logger.Printf("Error deleting torrent: %s", err)
	}
}

func (r *RealDebrid) GetDownloadLinks(torrent *Torrent) error {
	url := fmt.Sprintf("%s/unrestrict/link/", r.Host)
	downloadLinks := make([]TorrentDownloadLinks, 0)
	for _, link := range torrent.Links {
		if link == "" {
			continue
		}
		payload := gourl.Values{
			"link": {link},
		}
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload.Encode()))
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data structs.RealDebridUnrestrictResponse
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		download := TorrentDownloadLinks{
			Link:         data.Link,
			Filename:     data.Filename,
			DownloadLink: data.Download,
		}
		downloadLinks = append(downloadLinks, download)
	}
	torrent.DownloadLinks = downloadLinks
	return nil
}

func (r *RealDebrid) GetCheckCached() bool {
	return r.CheckCached
}

func NewRealDebrid(dc common.DebridConfig, cache *common.Cache) *RealDebrid {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	logger := common.NewLogger(dc.Name, os.Stdout)
	return &RealDebrid{
		BaseDebrid: BaseDebrid{
			Name:             "realdebrid",
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
