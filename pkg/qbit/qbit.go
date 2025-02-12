package qbit

import (
	"cmp"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"os"
)

type QBit struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	Storage         *TorrentStorage
	debug           bool
	logger          zerolog.Logger
	Tags            []string
	RefreshInterval int
}

func New() *QBit {
	cfg := config.GetConfig().QBitTorrent
	port := cmp.Or(cfg.Port, os.Getenv("QBIT_PORT"), "8282")
	refreshInterval := cmp.Or(cfg.RefreshInterval, 10)
	return &QBit{
		Username:        cfg.Username,
		Password:        cfg.Password,
		Port:            port,
		DownloadFolder:  cfg.DownloadFolder,
		Categories:      cfg.Categories,
		Storage:         NewTorrentStorage(cmp.Or(os.Getenv("TORRENT_FILE"), "/data/qbit_torrents.json")),
		logger:          logger.NewLogger("qbit", cfg.LogLevel, os.Stdout),
		RefreshInterval: refreshInterval,
	}
}
