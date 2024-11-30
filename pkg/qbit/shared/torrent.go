package shared

import (
	"cmp"
	"context"
	"fmt"
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/arr"
	"goBlack/pkg/debrid"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// All torrent related helpers goes here

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
	a, ok := ctx.Value("arr").(*arr.Arr)
	if !ok {
		return fmt.Errorf("arr not found in context")
	}
	isSymlink := ctx.Value("isSymlink").(bool)
	debridTorrent, err := debrid.ProcessTorrent(q.Debrid, magnet, a, isSymlink)
	if err != nil || debridTorrent == nil {
		if err == nil {
			err = fmt.Errorf("failed to process torrent")
		}
		return err
	}
	torrent = q.UpdateTorrentMin(torrent, debridTorrent)
	q.Storage.AddOrUpdate(torrent)
	go q.ProcessFiles(torrent, debridTorrent, a, isSymlink) // We can send async for file processing not to delay the response
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
		MagnetUri: magnet.Link,

		Tracker:    "udp://tracker.opentrackr.org:1337",
		UpLimit:    -1,
		DlLimit:    -1,
		AutoTmm:    false,
		Ratio:      1,
		RatioLimit: 1,
	}
	return torrent
}

func (q *QBit) ProcessFiles(torrent *Torrent, debridTorrent *debrid.Torrent, arr *arr.Arr, isSymlink bool) {
	for debridTorrent.Status != "downloaded" {
		progress := debridTorrent.Progress
		q.logger.Printf("%s Download Progress: %.2f%%", debridTorrent.Debrid.GetName(), progress)
		time.Sleep(5 * time.Second)
		dbT, err := debridTorrent.Debrid.CheckStatus(debridTorrent, isSymlink)
		if err != nil {
			q.logger.Printf("Error checking status: %v", err)
			q.MarkAsFailed(torrent)
			_ = arr.Refresh()
			return
		}
		debridTorrent = dbT
		torrent = q.UpdateTorrentMin(torrent, debridTorrent)
	}
	var (
		torrentPath string
		err         error
	)
	debridTorrent.Arr = arr
	if isSymlink {
		torrentPath, err = q.ProcessSymlink(debridTorrent)
	} else {
		torrentPath, err = q.processManualFiles(debridTorrent)
	}
	if err != nil {
		q.MarkAsFailed(torrent)
		go debridTorrent.Delete()
		q.logger.Printf("Error: %v", err)
		return
	}
	torrent.TorrentPath = filepath.Base(torrentPath)
	q.UpdateTorrent(torrent, debridTorrent)
	_ = arr.Refresh()
}

func (q *QBit) MarkAsFailed(t *Torrent) *Torrent {
	t.State = "error"
	q.Storage.AddOrUpdate(t)
	return t
}

func (q *QBit) UpdateTorrentMin(t *Torrent, debridTorrent *debrid.Torrent) *Torrent {
	if debridTorrent == nil {
		return t
	}

	addedOn, err := time.Parse(time.RFC3339, debridTorrent.Added)
	if err != nil {
		addedOn = time.Now()
	}
	totalSize := debridTorrent.Bytes
	progress := cmp.Or(debridTorrent.Progress, 100)
	progress = progress / 100.0
	sizeCompleted := int64(float64(totalSize) * progress)

	var speed int
	if debridTorrent.Speed != 0 {
		speed = debridTorrent.Speed
	}
	var eta int
	if speed != 0 {
		eta = int(totalSize-sizeCompleted) / speed
	}
	t.ID = debridTorrent.Id
	t.Name = debridTorrent.Name
	t.AddedOn = addedOn.Unix()
	t.DebridTorrent = debridTorrent
	t.Size = totalSize
	t.Completed = sizeCompleted
	t.Downloaded = sizeCompleted
	t.DownloadedSession = sizeCompleted
	t.Uploaded = sizeCompleted
	t.UploadedSession = sizeCompleted
	t.AmountLeft = totalSize - sizeCompleted
	t.Progress = progress
	t.Eta = eta
	t.Dlspeed = speed
	t.Upspeed = speed
	t.SavePath = filepath.Join(q.DownloadFolder, t.Category) + string(os.PathSeparator)
	t.ContentPath = filepath.Join(t.SavePath, t.Name) + string(os.PathSeparator)
	return t
}

func (q *QBit) UpdateTorrent(t *Torrent, debridTorrent *debrid.Torrent) *Torrent {
	db := debridTorrent.Debrid
	rcLoneMount := db.GetMountPath()
	if debridTorrent == nil && t.ID != "" {
		debridTorrent, _ = db.GetTorrent(t.ID)
	}
	if debridTorrent == nil {
		q.logger.Printf("Torrent with ID %s not found in %s", t.ID, db.GetName())
		return t
	}
	if debridTorrent.Status != "downloaded" {
		debridTorrent, _ = db.GetTorrent(t.ID)
	}

	if t.TorrentPath == "" {
		t.TorrentPath = filepath.Base(debridTorrent.GetMountFolder(rcLoneMount))
	}
	savePath := filepath.Join(q.DownloadFolder, t.Category) + string(os.PathSeparator)
	torrentPath := filepath.Join(savePath, t.TorrentPath) + string(os.PathSeparator)
	t = q.UpdateTorrentMin(t, debridTorrent)
	t.ContentPath = torrentPath

	if t.IsReady() {
		t.State = "pausedUP"
		q.Storage.Update(t)
		return t
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if t.IsReady() {
				t.State = "pausedUP"
				q.Storage.Update(t)
				return t
			}
			updatedT := q.UpdateTorrent(t, debridTorrent)
			t = updatedT

		case <-time.After(10 * time.Minute): // Add a timeout
			return t
		}
	}
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

func (q *QBit) GetTorrentProperties(t *Torrent) *TorrentProperties {
	return &TorrentProperties{
		AdditionDate:           t.AddedOn,
		Comment:                "Debrid Blackhole <https://github.com/sirrobot01/debrid-blackhole>",
		CreatedBy:              "Debrid Blackhole <https://github.com/sirrobot01/debrid-blackhole>",
		CreationDate:           t.AddedOn,
		DlLimit:                -1,
		UpLimit:                -1,
		DlSpeed:                t.Dlspeed,
		UpSpeed:                t.Upspeed,
		TotalSize:              t.Size,
		TotalUploaded:          t.Uploaded,
		TotalDownloaded:        t.Downloaded,
		TotalUploadedSession:   t.UploadedSession,
		TotalDownloadedSession: t.DownloadedSession,
		LastSeen:               time.Now().Unix(),
		NbConnectionsLimit:     100,
		Peers:                  0,
		PeersTotal:             2,
		SeedingTime:            1,
		Seeds:                  100,
		ShareRatio:             100,
	}
}

func (q *QBit) GetTorrentFiles(t *Torrent) []*TorrentFile {
	files := make([]*TorrentFile, 0)
	if t.DebridTorrent == nil {
		return files
	}
	for index, file := range t.DebridTorrent.Files {
		files = append(files, &TorrentFile{
			Index: index,
			Name:  file.Path,
			Size:  file.Size,
		})
	}
	return files
}
