package qbit

import (
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (q *QBit) Process(magnet *common.Magnet, category string) (*Torrent, error) {
	torrent := q.CreateTorrentFromMagnet(magnet, category)
	go q.storage.AddOrUpdate(torrent)
	debridTorrent, err := debrid.ProcessQBitTorrent(q.debrid, magnet, category)
	if err != nil || debridTorrent == nil {
		// Mark as failed
		q.logger.Printf("Failed to process torrent: %s: %v", magnet.Name, err)
		q.MarkAsFailed(torrent)
		return torrent, err
	}
	torrent.ID = debridTorrent.Id
	torrent.Name = debridTorrent.Name // Update the name before adding it to *arrs storage
	torrent.DebridTorrent = debridTorrent
	go q.processFiles(torrent, debridTorrent)
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

func (q *QBit) processFiles(torrent *Torrent, debridTorrent *debrid.Torrent) {
	var wg sync.WaitGroup
	files := debridTorrent.Files
	ready := make(chan debrid.TorrentFile, len(files))

	q.logger.Printf("Checking %d files...", len(files))
	rCloneMountPath := q.debrid.GetMountPath()
	path := filepath.Join(q.DownloadFolder, debridTorrent.Arr.CompletedFolder, debridTorrent.Folder) // /mnt/symlinks/{category}/MyTVShow/
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		q.logger.Printf("Failed to create directory: %s\n", path)
	}

	for _, file := range files {
		wg.Add(1)
		go checkFileLoop(&wg, rCloneMountPath, file, ready)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	for f := range ready {
		q.logger.Println("File is ready:", f.Path)
		q.createSymLink(path, debridTorrent, f)

	}
	// Update the torrent when all files are ready
	q.UpdateTorrent(torrent, debridTorrent)
	q.logger.Printf("%s COMPLETED \n", debridTorrent.Name)
}

func (q *QBit) createSymLink(path string, torrent *debrid.Torrent, file debrid.TorrentFile) {

	// Combine the directory and filename to form a full path
	fullPath := filepath.Join(path, file.Name) // /mnt/symlinks/{category}/MyTVShow/MyTVShow.S01E01.720p.mkv
	// Create a symbolic link if file doesn't exist
	torrentMountPath := filepath.Join(q.debrid.GetMountPath(), torrent.Folder, file.Name) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
	_ = os.Symlink(torrentMountPath, fullPath)
	// Check if the file exists
	if !fileReady(fullPath) {
		q.logger.Printf("Failed to create symlink: %s\n", fullPath)
	}
}
