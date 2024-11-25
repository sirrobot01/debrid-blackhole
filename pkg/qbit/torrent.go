package qbit

import (
	"cmp"
	"goBlack/pkg/debrid"
	"os"
	"path/filepath"
	"time"
)

// All torrent related helpers goes here

func (q *QBit) MarkAsFailed(t *Torrent) *Torrent {
	t.State = "error"
	q.storage.AddOrUpdate(t)
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
	totalSize := float64(debridTorrent.Bytes)
	progress := cmp.Or(debridTorrent.Progress, 100.0)
	progress = progress / 100.0
	sizeCompleted := int64(totalSize * progress)

	var speed int64
	if debridTorrent.Speed != 0 {
		speed = debridTorrent.Speed
	}
	var eta int64
	if speed != 0 {
		eta = int64((totalSize - float64(sizeCompleted)) / float64(speed))
	}
	t.ID = debridTorrent.Id
	t.Name = debridTorrent.Name
	t.AddedOn = addedOn.Unix()
	t.DebridTorrent = debridTorrent
	t.Size = int64(totalSize)
	t.Completed = sizeCompleted
	t.Downloaded = sizeCompleted
	t.DownloadedSession = sizeCompleted
	t.Uploaded = sizeCompleted
	t.UploadedSession = sizeCompleted
	t.AmountLeft = int64(totalSize) - sizeCompleted
	t.Progress = float32(progress)
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
		q.storage.Update(t)
		return t
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if t.IsReady() {
				t.State = "pausedUP"
				q.storage.Update(t)
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
