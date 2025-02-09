package shared

import (
	"cmp"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
	"os"
)

type QBit struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	Debrid          *debrid.DebridService
	Repair          *repair.Repair
	Storage         *TorrentStorage
	debug           bool
	logger          zerolog.Logger
	Arrs            *arr.Storage
	Tags            []string
	RefreshInterval int
}

func NewQBit(deb *debrid.DebridService, logger zerolog.Logger, arrs *arr.Storage, _repair *repair.Repair) *QBit {
	cfg := config.GetConfig().QBitTorrent
	port := cmp.Or(cfg.Port, os.Getenv("QBIT_PORT"), "8282")
	refreshInterval := cmp.Or(cfg.RefreshInterval, 10)
	return &QBit{
		Username:        cfg.Username,
		Password:        cfg.Password,
		Port:            port,
		DownloadFolder:  cfg.DownloadFolder,
		Categories:      cfg.Categories,
		Debrid:          deb,
		Storage:         NewTorrentStorage(cmp.Or(os.Getenv("TORRENT_FILE"), "/data/torrents.json")),
		Repair:          _repair,
		logger:          logger,
		Arrs:            arrs,
		RefreshInterval: refreshInterval,
	}
}
