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
	savePath := filepath.Join(q.DownloadFolder, t.Category) + string(os.PathSeparator)
	torrentPath := filepath.Join(savePath, debridTorrent.Folder) + string(os.PathSeparator)

	var speed int64
	if debridTorrent.Speed != 0 {
		speed = int64(debridTorrent.Speed)
	}
	var eta int64
	if speed != 0 {
		eta = (totalSize - sizeCompleted) / speed
	}

	t.Name = debridTorrent.Name
	t.Size = debridTorrent.Bytes
	t.Completed = sizeCompleted
	t.Downloaded = sizeCompleted
	t.DownloadedSession = sizeCompleted
	t.Uploaded = sizeCompleted
	t.UploadedSession = sizeCompleted
	t.AmountLeft = totalSize - sizeCompleted
	t.Progress = 100
	t.SavePath = savePath
	t.ContentPath = torrentPath
	t.Eta = eta
	t.Dlspeed = speed
	t.Upspeed = speed

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
