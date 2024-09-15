package qbit

import (
	"context"
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (q *QBit) Process(ctx context.Context, magnet *common.Magnet, category string) (*Torrent, error) {
	torrent := q.CreateTorrentFromMagnet(magnet, category)
	go q.storage.AddOrUpdate(torrent)
	arr := &debrid.Arr{
		Name:  category,
		Token: ctx.Value("token").(string),
		Host:  ctx.Value("host").(string),
	}
	debridTorrent, err := debrid.ProcessQBitTorrent(q.debrid, magnet, arr)
	if err != nil || debridTorrent == nil {
		// Mark as failed
		q.logger.Printf("Failed to process torrent: %s: %v", magnet.Name, err)
		q.MarkAsFailed(torrent)
		return torrent, err
	}
	torrent.ID = debridTorrent.Id
	torrent.DebridTorrent = debridTorrent
	torrent.Name = debridTorrent.Name
	q.processFiles(torrent, debridTorrent, arr)
	return torrent, nil
}

func (q *QBit) CreateTorrentFromMagnet(magnet *common.Magnet, category string) *Torrent {
	torrent := &Torrent{
		ID:        uuid.NewString(),
		Hash:      strings.ToLower(magnet.InfoHash),
		Name:      magnet.Name,
		Size:      magnet.Size,
		Category:  category,
		State:     "downloading",
		AddedOn:   time.Now().Unix(),
		MagnetUri: magnet.Link,

		Tracker:        "udp://tracker.opentrackr.org:1337",
		UpLimit:        -1,
		DlLimit:        -1,
		FlPiecePrio:    false,
		ForceStart:     false,
		AutoTmm:        false,
		Availability:   2,
		MaxRatio:       -1,
		MaxSeedingTime: -1,
		NumComplete:    10,
		NumIncomplete:  0,
		NumLeechs:      1,
		Ratio:          1,
		RatioLimit:     1,
	}
	return torrent
}

func (q *QBit) processFiles(torrent *Torrent, debridTorrent *debrid.Torrent, arr *debrid.Arr) {
	var wg sync.WaitGroup
	files := debridTorrent.Files
	ready := make(chan debrid.TorrentFile, len(files))

	q.logger.Printf("Checking %d files...", len(files))
	rCloneBase := q.debrid.GetMountPath()
	torrentPath, err := q.getTorrentPath(rCloneBase, debridTorrent) // /MyTVShow/
	if err != nil {
		q.logger.Printf("Error: %v", err)
		return
	}

	torrentSymlinkPath := filepath.Join(q.DownloadFolder, debridTorrent.Arr.Name, torrentPath) // /mnt/symlinks/{category}/MyTVShow/
	err = os.MkdirAll(torrentSymlinkPath, os.ModePerm)
	if err != nil {
		q.logger.Printf("Failed to create directory: %s\n", torrentSymlinkPath)
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
		q.logger.Printf("Failed to create symlink: %s\n", fullPath)
	}
	// Check if the file exists
	if !common.FileReady(fullPath) {
		q.logger.Printf("Symlink not ready: %s\n", fullPath)
	}
}
