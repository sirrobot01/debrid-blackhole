package qbit

import (
	"cmp"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"os"
	"path/filepath"
)

type QBit struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	Storage         *TorrentStorage
	logger          zerolog.Logger
	Tags            []string
	RefreshInterval int
	SkipPreCache    bool
}

func New() *QBit {
	_cfg := config.GetConfig()
	cfg := _cfg.QBitTorrent
	port := cmp.Or(cfg.Port, os.Getenv("QBIT_PORT"), "8282")
	refreshInterval := cmp.Or(cfg.RefreshInterval, 10)
	return &QBit{
		Username:        cfg.Username,
		Password:        cfg.Password,
		Port:            port,
		DownloadFolder:  cfg.DownloadFolder,
		Categories:      cfg.Categories,
		Storage:         NewTorrentStorage(filepath.Join(_cfg.Path, "torrents.json")),
		logger:          logger.NewLogger("qbit"),
		RefreshInterval: refreshInterval,
		SkipPreCache:    cfg.SkipPreCache,
	}
}
