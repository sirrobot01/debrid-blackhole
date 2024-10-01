package qbit

import (
	"goBlack/common"
	"goBlack/pkg/debrid"
	"goBlack/pkg/qbit/downloaders"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func (q *QBit) processManualFiles(torrent *Torrent, debridTorrent *debrid.Torrent, arr *debrid.Arr) {
	q.logger.Printf("Downloading %d files...", len(debridTorrent.DownloadLinks))
	torrentPath := common.RemoveExtension(debridTorrent.OriginalFilename)
	parent := common.RemoveInvalidChars(filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentPath))
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		q.logger.Printf("Failed to create directory: %s\n", parent)
		q.MarkAsFailed(torrent)
		return
	}
	torrent.TorrentPath = torrentPath
	q.downloadFiles(debridTorrent, parent)
	q.UpdateTorrent(torrent, debridTorrent)
	q.RefreshArr(arr)
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

func (q *QBit) processSymlink(torrent *Torrent, debridTorrent *debrid.Torrent, arr *debrid.Arr) {
	var wg sync.WaitGroup
	files := debridTorrent.Files
	ready := make(chan debrid.TorrentFile, len(files))

	q.logger.Printf("Checking %d files...", len(files))
	rCloneBase := q.debrid.GetMountPath()
	torrentPath, err := q.getTorrentPath(rCloneBase, debridTorrent) // /MyTVShow/
	if err != nil {
		q.MarkAsFailed(torrent)
		q.logger.Printf("Error: %v", err)
		return
	}

	torrentSymlinkPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentPath) // /mnt/symlinks/{category}/MyTVShow/
	err = os.MkdirAll(torrentSymlinkPath, os.ModePerm)
	if err != nil {
		q.logger.Printf("Failed to create directory: %s\n", torrentSymlinkPath)
		q.MarkAsFailed(torrent)
		return
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
	// Update the torrent when all files are ready
	torrent.TorrentPath = filepath.Base(torrentPath) // Quite important
	q.UpdateTorrent(torrent, debridTorrent)
	q.RefreshArr(arr)
}

func (q *QBit) getTorrentPath(rclonePath string, debridTorrent *debrid.Torrent) (string, error) {
	pathChan := make(chan string)
	errChan := make(chan error)

	go func() {
		for {
			torrentPath := debridTorrent.GetMountFolder(rclonePath)
			if torrentPath != "" {
				pathChan <- torrentPath
				return
			}
			time.Sleep(time.Second)
		}
	}()

	select {
	case path := <-pathChan:
		return path, nil
	case err := <-errChan:
		return "", err
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
