package qbit

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"io"
	"mime/multipart"
	"strings"
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
	isSymlink := ctx.Value("isSymlink").(bool)
	debridTorrent, err := debrid.ProcessQBitTorrent(q.debrid, magnet, arr, isSymlink)
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
	go q.processFiles(torrent, debridTorrent, arr, isSymlink) // We can send async for file processing not to delay the response
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

func (q *QBit) processFiles(torrent *Torrent, debridTorrent *debrid.Torrent, arr *debrid.Arr, isSymlink bool) {
	for debridTorrent.Status != "downloaded" {
		progress := debridTorrent.Progress
		q.logger.Printf("Progress: %.2f%%", progress)
		time.Sleep(5 * time.Second)
		dbT, err := debridTorrent.Debrid.CheckStatus(debridTorrent, isSymlink)
		if err != nil {
			q.logger.Printf("Error checking status: %v", err)
			q.MarkAsFailed(torrent)
			return
		}
		debridTorrent = dbT
	}
	if isSymlink {
		q.processSymlink(torrent, debridTorrent, arr)
	} else {
		q.processManualFiles(torrent, debridTorrent, arr)
	}
}
