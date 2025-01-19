package shared

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/downloaders"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func (q *QBit) processManualFiles(debridTorrent *debrid.Torrent) (string, error) {
	q.logger.Info().Msgf("Downloading %d files...", len(debridTorrent.DownloadLinks))
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
			q.logger.Info().Msgf("No download link found for %s", link.Filename)
			continue
		}
		wg.Add(1)
		semaphore <- struct{}{}
		go func(link debrid.TorrentDownloadLinks) {
			defer wg.Done()
			defer func() { <-semaphore }()
			err := downloaders.NormalHTTP(client, link.DownloadLink, filepath.Join(parent, link.Filename))
			if err != nil {
				q.logger.Info().Msgf("Error downloading %s: %v", link.DownloadLink, err)
			} else {
				q.logger.Info().Msgf("Downloaded %s successfully", link.DownloadLink)
			}
		}(link)
	}
	wg.Wait()
	q.logger.Info().Msgf("Downloaded all files for %s", debridTorrent.Name)
}

func (q *QBit) ProcessSymlink(debridTorrent *debrid.Torrent) (string, error) {
	var wg sync.WaitGroup
	files := debridTorrent.Files
	ready := make(chan debrid.TorrentFile, len(files))
	if len(files) == 0 {
		return "", fmt.Errorf("no video files found")
	}
	q.logger.Info().Msgf("Checking %d files...", len(files))
	rCloneBase := debridTorrent.Debrid.GetMountPath()
	torrentPath, err := q.getTorrentPath(rCloneBase, debridTorrent) // /MyTVShow/
	if err != nil {
		return "", fmt.Errorf("failed to get torrent path: %v", err)
	}
	// Fix for alldebrid
	newTorrentPath := torrentPath
	if newTorrentPath == "" {
		// Alldebrid at times doesn't return the parent folder for single file torrents
		newTorrentPath = common.RemoveExtension(debridTorrent.Name) // MyTVShow
	}
	torrentSymlinkPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, newTorrentPath) // /mnt/symlinks/{category}/MyTVShow/
	err = os.MkdirAll(torrentSymlinkPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to create directory: %s: %v", torrentSymlinkPath, err)
	}
	torrentRclonePath := filepath.Join(rCloneBase, torrentPath) // leave it as is
	q.logger.Debug().Msgf("Debrid torrent path: %s\nSymlink Path: %s", torrentRclonePath, torrentSymlinkPath)
	for _, file := range files {
		wg.Add(1)
		go checkFileLoop(&wg, torrentRclonePath, file, ready)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	for f := range ready {
		q.logger.Info().Msgf("File is ready: %s", f.Path)
		q.createSymLink(torrentSymlinkPath, torrentRclonePath, f)
	}
	return torrentPath, nil
}

func (q *QBit) getTorrentPath(rclonePath string, debridTorrent *debrid.Torrent) (string, error) {
	for {
		q.logger.Debug().Msgf("Checking for torrent path: %s", rclonePath)
		torrentPath, err := debridTorrent.GetMountFolder(rclonePath)
		if err == nil {
			q.logger.Debug().Msgf("Found torrent path: %s", torrentPath)
			return torrentPath, err
		}
		time.Sleep(time.Second)
	}
}

func (q *QBit) createSymLink(path string, torrentMountPath string, file debrid.TorrentFile) {

	// Combine the directory and filename to form a full path
	fullPath := filepath.Join(path, file.Name) // /mnt/symlinks/{category}/MyTVShow/MyTVShow.S01E01.720p.mkv
	// Create a symbolic link if file doesn't exist
	torrentFilePath := filepath.Join(torrentMountPath, file.Path) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
	err := os.Symlink(torrentFilePath, fullPath)
	if err != nil {
		q.logger.Info().Msgf("Failed to create symlink: %s: %v", fullPath, err)
	}
}
