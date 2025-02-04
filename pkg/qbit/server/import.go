package server

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"time"

	"github.com/google/uuid"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/qbit/shared"
)

type ImportRequest struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	URI       string   `json:"uri"`
	Arr       *arr.Arr `json:"arr"`
	IsSymlink bool     `json:"isSymlink"`
	SeriesId  int      `json:"series"`
	Seasons   []int    `json:"seasons"`
	Episodes  []string `json:"episodes"`

	Failed      bool      `json:"failed"`
	FailedAt    time.Time `json:"failedAt"`
	Reason      string    `json:"reason"`
	Completed   bool      `json:"completed"`
	CompletedAt time.Time `json:"completedAt"`
	Async       bool      `json:"async"`
}

type ManualImportResponseSchema struct {
	Priority            string    `json:"priority"`
	Status              string    `json:"status"`
	Result              string    `json:"result"`
	Queued              time.Time `json:"queued"`
	Trigger             string    `json:"trigger"`
	SendUpdatesToClient bool      `json:"sendUpdatesToClient"`
	UpdateScheduledTask bool      `json:"updateScheduledTask"`
	Id                  int       `json:"id"`
}

func NewImportRequest(uri string, arr *arr.Arr, isSymlink bool) *ImportRequest {
	return &ImportRequest{
		ID:        uuid.NewString(),
		URI:       uri,
		Arr:       arr,
		Failed:    false,
		Completed: false,
		Async:     false,
		IsSymlink: isSymlink,
	}
}

func (i *ImportRequest) Fail(reason string) {
	i.Failed = true
	i.FailedAt = time.Now()
	i.Reason = reason
}

func (i *ImportRequest) Complete() {
	i.Completed = true
	i.CompletedAt = time.Now()
}

func (i *ImportRequest) Process(q *shared.QBit) (err error) {
	// Use this for now.
	// This sends the torrent to the arr
	magnet, err := utils.GetMagnetFromUrl(i.URI)
	if err != nil {
		return fmt.Errorf("error parsing magnet link: %w", err)
	}
	torrent := q.CreateTorrentFromMagnet(magnet, i.Arr.Name, "manual")
	debridTorrent, err := debrid.ProcessTorrent(q.Debrid, magnet, i.Arr, i.IsSymlink)
	if err != nil || debridTorrent == nil {
		if debridTorrent != nil {
			go debridTorrent.Delete()
		}
		if err == nil {
			err = fmt.Errorf("failed to process torrent")
		}
		return err
	}
	torrent = q.UpdateTorrentMin(torrent, debridTorrent)
	q.Storage.AddOrUpdate(torrent)
	go q.ProcessFiles(torrent, debridTorrent, i.Arr, i.IsSymlink)
	return nil
}
