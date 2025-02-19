package qbit

import (
	"crypto/tls"
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	debrid "github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func Download(client *grab.Client, url, filename string, progressCallback func(int64, int64)) error {
	req, err := grab.NewRequest(filename, url)
	if err != nil {
		return err
	}
	resp := client.Do(req)

	t := time.NewTicker(time.Second)
	defer t.Stop()

	var lastReported int64
Loop:
	for {
		select {
		case <-t.C:
			current := resp.BytesComplete()
			speed := int64(resp.BytesPerSecond())
			if current != lastReported {
				if progressCallback != nil {
					progressCallback(current-lastReported, speed)
				}
				lastReported = current
			}
		case <-resp.Done:
			break Loop
		}
	}

	// Report final bytes
	if progressCallback != nil {
		progressCallback(resp.BytesComplete()-lastReported, 0)
	}

	return resp.Err()
}

func (q *QBit) ProcessManualFile(torrent *Torrent) (string, error) {
	debridTorrent := torrent.DebridTorrent
	q.logger.Info().Msgf("Downloading %d files...", len(debridTorrent.DownloadLinks))
	torrentPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, utils.RemoveExtension(debridTorrent.OriginalFilename))
	torrentPath = utils.RemoveInvalidChars(torrentPath)
	err := os.MkdirAll(torrentPath, os.ModePerm)
	if err != nil {
		// add previous error to the error and return
		return "", fmt.Errorf("failed to create directory: %s: %v", torrentPath, err)
	}
	q.downloadFiles(torrent, torrentPath)
	return torrentPath, nil
}

func (q *QBit) downloadFiles(torrent *Torrent, parent string) {
	debridTorrent := torrent.DebridTorrent
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)
	totalSize := int64(0)
	for _, file := range debridTorrent.Files {
		totalSize += file.Size
	}
	debridTorrent.Mu.Lock()
	debridTorrent.SizeDownloaded = 0 // Reset downloaded bytes
	debridTorrent.Progress = 0       // Reset progress
	debridTorrent.Mu.Unlock()
	progressCallback := func(downloaded int64, speed int64) {
		debridTorrent.Mu.Lock()
		defer debridTorrent.Mu.Unlock()
		torrent.Mu.Lock()
		defer torrent.Mu.Unlock()

		// Update total downloaded bytes
		debridTorrent.SizeDownloaded += downloaded
		debridTorrent.Speed = speed

		// Calculate overall progress
		if totalSize > 0 {
			debridTorrent.Progress = float64(debridTorrent.SizeDownloaded) / float64(totalSize) * 100
		}
		q.UpdateTorrentMin(torrent, debridTorrent)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyFromEnvironment,
	}
	client := &grab.Client{
		UserAgent: "qBitTorrent",
		HTTPClient: &http.Client{
			Transport: tr,
		},
	}
	for _, link := range debridTorrent.DownloadLinks {
		if link.DownloadLink == "" {
			q.logger.Info().Msgf("No download link found for %s", link.Filename)
			continue
		}
		wg.Add(1)
		semaphore <- struct{}{}
		go func(link debrid.DownloadLinks) {
			defer wg.Done()
			defer func() { <-semaphore }()
			filename := link.Filename

			err := Download(
				client,
				link.DownloadLink,
				filepath.Join(parent, filename),
				progressCallback,
			)

			if err != nil {
				q.logger.Error().Msgf("Failed to download %s: %v", filename, err)
			} else {
				q.logger.Info().Msgf("Downloaded %s", filename)
			}
		}(link)
	}
	wg.Wait()
	q.logger.Info().Msgf("Downloaded all files for %s", debridTorrent.Name)
}

func (q *QBit) ProcessSymlink(torrent *Torrent) (string, error) {
	debridTorrent := torrent.DebridTorrent
	files := debridTorrent.Files
	if len(files) == 0 {
		return "", fmt.Errorf("no video files found")
	}
	q.logger.Info().Msgf("Checking symlinks for %d files...", len(files))
	rCloneBase := debridTorrent.MountPath
	torrentPath, err := q.getTorrentPath(rCloneBase, debridTorrent) // /MyTVShow/
	// This returns filename.ext for alldebrid instead of the parent folder filename/
	torrentFolder := torrentPath
	if err != nil {
		return "", fmt.Errorf("failed to get torrent path: %v", err)
	}
	// Check if the torrent path is a file
	torrentRclonePath := filepath.Join(rCloneBase, torrentPath) // leave it as is
	if debridTorrent.Debrid == "alldebrid" && utils.IsMediaFile(torrentPath) {
		// Alldebrid hotfix for single file torrents
		torrentFolder = utils.RemoveExtension(torrentFolder)
		torrentRclonePath = rCloneBase // /mnt/rclone/magnets/  // Remove the filename since it's in the root folder
	}
	torrentSymlinkPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentFolder) // /mnt/symlinks/{category}/MyTVShow/
	err = os.MkdirAll(torrentSymlinkPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create directory: %s: %v", torrentSymlinkPath, err)
	}

	pending := make(map[string]debrid.File)
	for _, file := range files {
		pending[file.Path] = file
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for len(pending) > 0 {
		<-ticker.C
		for path, file := range pending {
			fullFilePath := filepath.Join(torrentRclonePath, file.Path)
			if _, err := os.Stat(fullFilePath); !os.IsNotExist(err) {
				q.logger.Info().Msgf("File is ready: %s", file.Path)
				q.createSymLink(torrentSymlinkPath, torrentRclonePath, file)
				delete(pending, path)
			}
		}
	}
	return torrentSymlinkPath, nil
}

func (q *QBit) getTorrentPath(rclonePath string, debridTorrent *debrid.Torrent) (string, error) {
	for {
		torrentPath, err := debridTorrent.GetMountFolder(rclonePath)
		if err == nil {
			q.logger.Debug().Msgf("Found torrent path: %s", torrentPath)
			return torrentPath, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (q *QBit) createSymLink(path string, torrentMountPath string, file debrid.File) {

	// Combine the directory and filename to form a full path
	fullPath := filepath.Join(path, file.Name) // /mnt/symlinks/{category}/MyTVShow/MyTVShow.S01E01.720p.mkv
	// Create a symbolic link if file doesn't exist
	torrentFilePath := filepath.Join(torrentMountPath, file.Path) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
	err := os.Symlink(torrentFilePath, fullPath)
	if err != nil {
		// It's okay if the symlink already exists
		q.logger.Debug().Msgf("Failed to create symlink: %s: %v", fullPath, err)
	}
}
