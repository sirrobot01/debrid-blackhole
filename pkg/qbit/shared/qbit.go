package shared

import (
	"cmp"
	"goBlack/common"
	"goBlack/pkg/arr"
	"goBlack/pkg/debrid"
	"log"
	"os"
)

type QBit struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	Debrid          *debrid.DebridService
	cache           *common.Cache
	Storage         *TorrentStorage
	debug           bool
	logger          *log.Logger
	Arrs            *arr.Storage
	RefreshInterval int
}

func NewQBit(config *common.Config, deb *debrid.DebridService, cache *common.Cache, logger *log.Logger) *QBit {
	cfg := config.QBitTorrent
	port := cmp.Or(cfg.Port, os.Getenv("QBIT_PORT"), "8182")
	refreshInterval := cmp.Or(cfg.RefreshInterval, 10)
	arrs := arr.NewStorage()
	return &QBit{
		Username:        cfg.Username,
		Password:        cfg.Password,
		Port:            port,
		DownloadFolder:  cfg.DownloadFolder,
		Categories:      cfg.Categories,
		Debrid:          deb,
		cache:           cache,
		debug:           cfg.Debug,
		Storage:         NewTorrentStorage("torrents.json"),
		logger:          logger,
		Arrs:            arrs,
		RefreshInterval: refreshInterval,
	}
}
