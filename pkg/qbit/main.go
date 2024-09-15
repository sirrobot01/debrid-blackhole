package qbit

import (
	"cmp"
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"log"
	"net/http"
	"os"
	"time"
)

type WorkerType struct {
	ticker *time.Ticker
	ctx    context.Context
}

type Worker struct {
	types map[string]WorkerType
}

type QBit struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	debrid          debrid.Service
	cache           *common.Cache
	storage         *TorrentStorage
	debug           bool
	logger          *log.Logger
	arrs            map[string]string // host:token (Used for refreshing in worker)
	RefreshInterval int
}

func NewQBit(config *common.Config, deb debrid.Service, cache *common.Cache) *QBit {
	cfg := config.QBitTorrent
	storage := NewTorrentStorage("torrents.json")
	port := cmp.Or(cfg.Port, os.Getenv("QBIT_PORT"), "8182")
	refreshInterval := cmp.Or(cfg.RefreshInterval, 10)
	return &QBit{
		Username:        cfg.Username,
		Password:        cfg.Password,
		Port:            port,
		DownloadFolder:  cfg.DownloadFolder,
		Categories:      cfg.Categories,
		debrid:          deb,
		cache:           cache,
		debug:           cfg.Debug,
		storage:         storage,
		logger:          common.NewLogger("QBit", os.Stdout),
		arrs:            make(map[string]string),
		RefreshInterval: refreshInterval,
	}
}

func (q *QBit) Start() {

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	q.AddRoutes(r)

	ctx := context.Background()

	go q.StartWorker(ctx)

	q.logger.Printf("Starting QBit server on :%s", q.Port)
	port := fmt.Sprintf(":%s", q.Port)
	q.logger.Fatal(http.ListenAndServe(port, r))
}
