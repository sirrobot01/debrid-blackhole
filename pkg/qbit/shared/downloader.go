package shared

import (
	"fmt"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/downloaders"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func (q *QBit) processManualFiles(debridTorrent *debrid.Torrent) (string, error) {
	q.logger.Printf("Downloading %d files...", len(debridTorrent.DownloadLinks))
	torrentPath := common.RemoveExtension(debridTorrent.OriginalFilename)
	parent := common.RemoveInvalidChars(filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentPath))
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		// add previous error to the error and return
		return "", fmt.Errorf("failed to create directory: %s: %v", parent, err)
	}
	q.downloadFiles(debridTorrent, parent)
	return torrentPath, nil
}

func (q *QBit) downloadFiles(debridTorrent *debrid.Torrent, parent string) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5)
	client := downloaders.GetHTTPClient()
	for _, link := range debridTorrent.DownloadLinks {
		if link.DownloadLink == "" {
			q.logger.Printf("No download link found for %s\n", link.Filename)
			continue
		}
		wg.Add(1)
		semaphore <- struct{}{}
		go func(link debrid.TorrentDownloadLinks) {
			defer wg.Done()
			defer func() { <-semaphore }()
			err := downloaders.NormalHTTP(client, link.DownloadLink, filepath.Join(parent, link.Filename))
			if err != nil {
				q.logger.Printf("Error downloading %s: %v\n", link.DownloadLink, err)
			} else {
				q.logger.Printf("Downloaded %s successfully\n", link.DownloadLink)
			}
		}(link)
	}
	wg.Wait()
	q.logger.Printf("Downloaded all files for %s\n", debridTorrent.Name)
}

func (q *QBit) ProcessSymlink(debridTorrent *debrid.Torrent) (string, error) {
	var wg sync.WaitGroup
	files := debridTorrent.Files
	ready := make(chan debrid.TorrentFile, len(files))

	q.logger.Printf("Checking %d files...", len(files))
	rCloneBase := debridTorrent.Debrid.GetMountPath()
	torrentPath, err := q.getTorrentPath(rCloneBase, debridTorrent) // /MyTVShow/
	if err != nil {
		return "", fmt.Errorf("failed to get torrent path: %v", err)
	}
	torrentSymlinkPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentPath) // /mnt/symlinks/{category}/MyTVShow/
	err = os.MkdirAll(torrentSymlinkPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create directory: %s: %v", torrentSymlinkPath, err)
	}
	torrentRclonePath := filepath.Join(rCloneBase, torrentPath)
	for _, file := range files {
		wg.Add(1)
		go checkFileLoop(&wg, torrentRclonePath, file, ready)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	for f := range ready {
		q.logger.Println("File is ready:", f.Path)
		q.createSymLink(torrentSymlinkPath, torrentRclonePath, f)
	}
	return torrentPath, nil
}

func (q *QBit) getTorrentPath(rclonePath string, debridTorrent *debrid.Torrent) (string, error) {
	for {
		torrentPath := debridTorrent.GetMountFolder(rclonePath)
		if torrentPath != "" {
			return torrentPath, nil
		}
		time.Sleep(time.Second)
	}
}

func (q *QBit) createSymLink(path string, torrentMountPath string, file debrid.TorrentFile) {

	// Combine the directory and filename to form a full path
	fullPath := filepath.Join(path, file.Name) // /mnt/symlinks/{category}/MyTVShow/MyTVShow.S01E01.720p.mkv
	// Create a symbolic link if file doesn't exist
	torrentFilePath := filepath.Join(torrentMountPath, file.Name) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
	err := os.Symlink(torrentFilePath, fullPath)
	if err != nil {
		q.logger.Printf("Failed to create symlink: %s: %v\n", fullPath, err)
	}
	// Check if the file exists
	if !common.FileReady(fullPath) {
		q.logger.Printf("Symlink not ready: %s\n", fullPath)
	}
}
