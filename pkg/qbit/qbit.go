package qbit

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (q *QBit) AddMagnet(ctx context.Context, url, category string) error {
	magnet, err := common.GetMagnetFromUrl(url)
	if err != nil {
		q.logger.Printf("Error parsing magnet link: %v\n", err)
		return err
	}
	err = q.Process(ctx, magnet, category)
	if err != nil {
		q.logger.Println("Failed to process magnet:", err)
		return err
	}
	return nil
}

func (q *QBit) AddTorrent(ctx context.Context, fileHeader *multipart.FileHeader, category string) error {
	file, _ := fileHeader.Open()
	defer file.Close()
	var reader io.Reader = file
	magnet, err := common.GetMagnetFromFile(reader, fileHeader.Filename)
	if err != nil {
		q.logger.Printf("Error reading file: %s", fileHeader.Filename)
		return err
	}
	err = q.Process(ctx, magnet, category)
	if err != nil {
		q.logger.Println("Failed to process torrent:", err)
		return err
	}
	return nil
}

func (q *QBit) Process(ctx context.Context, magnet *common.Magnet, category string) error {
	torrent := q.CreateTorrentFromMagnet(magnet, category)
	arr := &debrid.Arr{
		Name:  category,
		Token: ctx.Value("token").(string),
		Host:  ctx.Value("host").(string),
	}
	debridTorrent, err := debrid.ProcessQBitTorrent(q.debrid, magnet, arr)
	if err != nil || debridTorrent == nil {
		if err == nil {
			err = fmt.Errorf("failed to process torrent")
		}
		return err
	}
	torrent.ID = debridTorrent.Id
	torrent.DebridTorrent = debridTorrent
	torrent.Name = debridTorrent.Name
	q.storage.AddOrUpdate(torrent)
	go q.processFiles(torrent, debridTorrent, arr) // We can send async for file processing not to delay the response
	return nil
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
		q.logger.Printf("Failed to create symlink: %s\n", fullPath)
	}
	// Check if the file exists
	if !common.FileReady(fullPath) {
		q.logger.Printf("Symlink not ready: %s\n", fullPath)
	}
}
