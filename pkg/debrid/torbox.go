package debrid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"goBlack/common"
	"goBlack/pkg/debrid/structs"
	"log"
	"mime/multipart"
	"net/http"
	gourl "net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type Torbox struct {
	BaseDebrid
}

func (r *Torbox) GetMountPath() string {
	return r.MountPath
}

func (r *Torbox) GetName() string {
	return r.Name
}

func (r *Torbox) GetLogger() *log.Logger {
	return r.logger
}

func (r *Torbox) IsAvailable(infohashes []string) map[string]bool {
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
		url := fmt.Sprintf("%s/api/torrents/checkcached?hash=%s", r.Host, hashStr)
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			log.Println("Error checking availability:", err)
			return result
		}
		var res structs.TorBoxAvailableResponse
		err = json.Unmarshal(resp, &res)
		if err != nil {
			log.Println("Error marshalling availability:", err)
			return result
		}
		if res.Data == nil {
			return result
		}

		for h, cache := range *res.Data {
			if cache.Size > 0 {
				result[strings.ToUpper(h)] = true
			}
		}
	}
	r.cache.AddMultiple(result) // Add the results to the cache
	return result
}

func (r *Torbox) SubmitMagnet(torrent *Torrent) (*Torrent, error) {
	url := fmt.Sprintf("%s/api/torrents/createtorrent", r.Host)
	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	_ = writer.WriteField("magnet", torrent.Magnet.Link)
	err := writer.Close()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodPost, url, payload)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return nil, err
	}
	var data structs.TorBoxAddMagnetResponse
	err = json.Unmarshal(resp, &data)
	if err != nil {
		return nil, err
	}
	if data.Data == nil {
		return nil, fmt.Errorf("error adding torrent")
	}
	dt := *data.Data
	torrentId := strconv.Itoa(dt.Id)
	log.Printf("Torrent: %s added with id: %s\n", torrent.Name, torrentId)
	torrent.Id = torrentId

	return torrent, nil
}

func getStatus(status string, finished bool) string {
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

func (r *Torbox) GetTorrent(id string) (*Torrent, error) {
	torrent := &Torrent{}
	url := fmt.Sprintf("%s/api/torrents/mylist/?id=%s", r.Host, id)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := r.client.MakeRequest(req)
	if err != nil {
		return torrent, err
	}
	var res structs.TorboxInfoResponse
	err = json.Unmarshal(resp, &res)
	if err != nil {
		return torrent, err
	}
	data := res.Data
	name := data.Name
	torrent.Id = id
	torrent.Name = name
	torrent.Bytes = data.Size
	torrent.Folder = name
	torrent.Progress = data.Progress * 100
	torrent.Status = getStatus(data.DownloadState, data.DownloadFinished)
	torrent.Speed = data.DownloadSpeed
	torrent.Seeders = data.Seeds
	torrent.Filename = name
	torrent.OriginalFilename = name
	files := make([]TorrentFile, 0)
	if len(data.Files) == 0 {
		return torrent, fmt.Errorf("no files found for torrent: %s", name)
	}
	for _, f := range data.Files {
		fileName := filepath.Base(f.Name)
		if (!common.RegexMatch(common.VIDEOMATCH, fileName) &&
			!common.RegexMatch(common.SUBMATCH, fileName) &&
			!common.RegexMatch(common.MUSICMATCH, fileName)) || common.RegexMatch(common.SAMPLEMATCH, fileName) {
			continue
		}
		file := TorrentFile{
			Id:   strconv.Itoa(f.Id),
			Name: fileName,
			Size: f.Size,
			Path: fileName,
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return torrent, fmt.Errorf("no video files found")
	}
	cleanPath := path.Clean(data.Files[0].Name)
	torrent.OriginalFilename = strings.Split(cleanPath, "/")[0]
	torrent.Files = files
	torrent.Debrid = r
	return torrent, nil
}

func (r *Torbox) CheckStatus(torrent *Torrent, isSymlink bool) (*Torrent, error) {
	for {
		tb, err := r.GetTorrent(torrent.Id)

		torrent = tb

		if err != nil || tb == nil {
			return tb, err
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

func (r *Torbox) DeleteTorrent(torrent *Torrent) {
	url := fmt.Sprintf("%s/api//torrents/controltorrent/%s", r.Host, torrent.Id)
	payload := map[string]string{"torrent_id": torrent.Id, "action": "Delete"}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodDelete, url, bytes.NewBuffer(jsonPayload))
	_, err := r.client.MakeRequest(req)
	if err == nil {
		r.logger.Printf("Torrent: %s deleted\n", torrent.Name)
	} else {
		r.logger.Printf("Error deleting torrent: %s", err)
	}
}

func (r *Torbox) GetDownloadLinks(torrent *Torrent) error {
	downloadLinks := make([]TorrentDownloadLinks, 0)
	for _, file := range torrent.Files {
		url := fmt.Sprintf("%s/api/torrents/requestdl/", r.Host)
		query := gourl.Values{}
		query.Add("torrent_id", torrent.Id)
		query.Add("token", r.APIKey)
		query.Add("file_id", file.Id)
		url += "?" + query.Encode()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := r.client.MakeRequest(req)
		if err != nil {
			return err
		}
		var data structs.TorBoxDownloadLinksResponse
		if err = json.Unmarshal(resp, &data); err != nil {
			return err
		}
		if data.Data == nil {
			return fmt.Errorf("error getting download links")
		}
		idx := 0
		link := *data.Data

		dl := TorrentDownloadLinks{
			Link:         link,
			Filename:     torrent.Files[idx].Name,
			DownloadLink: link,
		}
		downloadLinks = append(downloadLinks, dl)
	}
	torrent.DownloadLinks = downloadLinks
	return nil
}

func (r *Torbox) GetCheckCached() bool {
	return r.CheckCached
}

func NewTorbox(dc common.DebridConfig, cache *common.Cache) *Torbox {
	rl := common.ParseRateLimit(dc.RateLimit)
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", dc.APIKey),
	}
	client := common.NewRLHTTPClient(rl, headers)
	logger := common.NewLogger(dc.Name, os.Stdout)
	return &Torbox{
		BaseDebrid: BaseDebrid{
			Name:             "torbox",
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
