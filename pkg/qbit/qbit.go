package qbit

import (
	"cmp"
	"fmt"
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
		q.MarkAsFailed(torrent)
		return torrent, err
	}
	torrent.ID = debridTorrent.Id
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

	for _, file := range files {
		wg.Add(1)
		go checkFileLoop(&wg, rCloneMountPath, file, ready)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	for r := range ready {
		q.logger.Println("File is ready:", r.Name)
		q.createSymLink(debridTorrent)

	}
	q.UpdateTorrent(torrent, debridTorrent)
	fmt.Printf("%s downloaded \n", debridTorrent.Name)
}

func (q *QBit) createSymLink(torrent *debrid.Torrent) {
	path := torrent.GetSymlinkFolder(q.DownloadFolder) // /mnt/symlinks/{category}/MyTVShow/
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		q.logger.Printf("Failed to create directory: %s\n", path)
	}

	for _, file := range torrent.Files {
		// Combine the directory and filename to form a full path
		fullPath := filepath.Join(path, file.Name) // /mnt/symlinks/{category}/MyTVShow/MyTVShow.S01E01.720p.mkv
		// Create a symbolic link if file doesn't exist
		torrentMountPath := filepath.Join(q.debrid.GetMountPath(), torrent.Folder, file.Name) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
		_ = os.Symlink(torrentMountPath, fullPath)
	}
}

func (q *QBit) MarkAsFailed(t *Torrent) *Torrent {
	t.State = "error"
	q.storage.AddOrUpdate(t)
	return t
}

func (q *QBit) UpdateTorrent(t *Torrent, debridTorrent *debrid.Torrent) *Torrent {
	if debridTorrent == nil && t.ID != "" {
		debridTorrent, _ = q.debrid.GetTorrent(t.ID)
	}
	if debridTorrent == nil {
		q.logger.Printf("Torrent with ID %s not found in %s", t.ID, q.debrid.GetName())
		return t
	}
	totalSize := cmp.Or(debridTorrent.Bytes, 1)
	progress := int64(cmp.Or(debridTorrent.Progress, 100))
	progress = progress / 100.0

	sizeCompleted := totalSize * progress
	savePath := filepath.Join(q.DownloadFolder, t.Category)
	torrentPath := filepath.Join(savePath, t.Name)

	t.Size = debridTorrent.Bytes
	t.Completed = sizeCompleted
	t.Downloaded = sizeCompleted
	t.DownloadedSession = sizeCompleted
	t.Uploaded = sizeCompleted
	t.UploadedSession = sizeCompleted
	t.AmountLeft = totalSize - sizeCompleted
	t.Availability = 2
	t.Progress = 100
	t.SavePath = savePath
	t.ContentPath = torrentPath

	if t.AmountLeft == 0 {
		t.State = "pausedUP"
	}

	go q.storage.AddOrUpdate(t)
	return t
}

func (q *QBit) ResumeTorrent(t *Torrent) bool {
	return true
}

func (q *QBit) PauseTorrent(t *Torrent) bool {
	return true
}

func (q *QBit) RefreshTorrent(t *Torrent) bool {
	return true
}
