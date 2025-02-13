package cache

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

type DownloadLinkCache struct {
	Link string `json:"download_link"`
}

type CachedTorrent struct {
	*torrent.Torrent
	LastRead      time.Time                    `json:"last_read"`
	IsComplete    bool                         `json:"is_complete"`
	DownloadLinks map[string]DownloadLinkCache `json:"download_links"`
}

var (
	_logInstance zerolog.Logger
	once         sync.Once
)

func getLogger() zerolog.Logger {
	once.Do(func() {
		_logInstance = logger.NewLogger("cache", "info", os.Stdout)
	})
	return _logInstance
}

type Cache struct {
	dir           string
	client        engine.Service
	torrents      *sync.Map // key: torrent.Id, value: *CachedTorrent
	torrentsNames *sync.Map // key: torrent.Name, value: torrent.Id
	LastUpdated   time.Time `json:"last_updated"`
}

type Manager struct {
	caches map[string]*Cache
}

func NewManager(debridService *engine.Engine) *Manager {
	cfg := config.GetConfig()
	cm := &Manager{
		caches: make(map[string]*Cache),
	}
	for _, debrid := range debridService.GetDebrids() {
		c := New(debrid, cfg.Path)
		cm.caches[debrid.GetName()] = c
	}
	return cm
}

func (m *Manager) GetCaches() map[string]*Cache {
	return m.caches
}

func (m *Manager) GetCache(debridName string) *Cache {
	return m.caches[debridName]
}

func New(debridService engine.Service, basePath string) *Cache {
	return &Cache{
		dir:           filepath.Join(basePath, "cache", debridService.GetName(), "torrents"),
		torrents:      &sync.Map{},
		torrentsNames: &sync.Map{},
		client:        debridService,
	}
}

func (c *Cache) Start() error {
	_logger := getLogger()
	_logger.Info().Msg("Starting cache for: " + c.client.GetName())
	if err := c.Load(); err != nil {
		return fmt.Errorf("failed to load cache: %v", err)
	}
	if err := c.Sync(); err != nil {
		return fmt.Errorf("failed to sync cache: %v", err)
	}
	return nil
}

func (c *Cache) Load() error {
	_logger := getLogger()

	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	files, err := os.ReadDir(c.dir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(c.dir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			_logger.Debug().Err(err).Msgf("Failed to read file: %s", filePath)
			continue
		}

		var ct CachedTorrent
		if err := json.Unmarshal(data, &ct); err != nil {
			_logger.Debug().Err(err).Msgf("Failed to unmarshal file: %s", filePath)
			continue
		}
		if len(ct.Files) > 0 {
			c.torrents.Store(ct.Torrent.Id, &ct)
			c.torrentsNames.Store(ct.Torrent.Name, ct.Torrent.Id)
		}
	}

	return nil
}

func (c *Cache) GetTorrent(id string) *CachedTorrent {
	if value, ok := c.torrents.Load(id); ok {
		return value.(*CachedTorrent)
	}
	return nil
}

func (c *Cache) GetTorrentByName(name string) *CachedTorrent {
	if id, ok := c.torrentsNames.Load(name); ok {
		return c.GetTorrent(id.(string))
	}
	return nil
}

func (c *Cache) SaveTorrent(ct *CachedTorrent) error {
	data, err := json.MarshalIndent(ct, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal torrent: %w", err)
	}

	fileName := ct.Torrent.Id + ".json"
	filePath := filepath.Join(c.dir, fileName)
	tmpFile := filePath + ".tmp"

	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to flush data: %w", err)
	}

	return os.Rename(tmpFile, filePath)
}

func (c *Cache) SaveAll() error {
	const batchSize = 100
	var wg sync.WaitGroup
	_logger := getLogger()

	tasks := make(chan *CachedTorrent, batchSize)

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ct := range tasks {
				if err := c.SaveTorrent(ct); err != nil {
					_logger.Error().Err(err).Msg("failed to save torrent")
				}
			}
		}()
	}

	c.torrents.Range(func(_, value interface{}) bool {
		tasks <- value.(*CachedTorrent)
		return true
	})

	close(tasks)
	wg.Wait()
	c.LastUpdated = time.Now()
	return nil
}

func (c *Cache) Sync() error {
	_logger := getLogger()
	torrents, err := c.client.GetTorrents()
	if err != nil {
		return fmt.Errorf("failed to sync torrents: %v", err)
	}

	workers := runtime.NumCPU() * 200
	workChan := make(chan *torrent.Torrent, len(torrents))
	errChan := make(chan error, len(torrents))

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range workChan {
				if err := c.processTorrent(t); err != nil {
					errChan <- err
				}
			}
		}()
	}

	for _, t := range torrents {
		workChan <- t
	}
	close(workChan)

	wg.Wait()
	close(errChan)

	for err := range errChan {
		_logger.Error().Err(err).Msg("sync error")
	}

	_logger.Info().Msgf("Synced %d torrents", len(torrents))
	return nil
}

func (c *Cache) processTorrent(t *torrent.Torrent) error {
	if existing, ok := c.torrents.Load(t.Id); ok {
		ct := existing.(*CachedTorrent)
		if ct.IsComplete {
			return nil
		}
	}
	c.AddTorrent(t)
	return nil
}

func (c *Cache) AddTorrent(t *torrent.Torrent) {
	_logger := getLogger()

	if len(t.Files) == 0 {
		tNew, err := c.client.GetTorrent(t.Id)
		_logger.Debug().Msgf("Getting torrent files for %s", t.Id)
		if err != nil {
			_logger.Debug().Msgf("Failed to get torrent files for %s: %v", t.Id, err)
			return
		}
		t = tNew
	}

	if len(t.Files) == 0 {
		_logger.Debug().Msgf("No files found for %s", t.Id)
		return
	}

	ct := &CachedTorrent{
		Torrent:       t,
		LastRead:      time.Now(),
		IsComplete:    len(t.Files) > 0,
		DownloadLinks: make(map[string]DownloadLinkCache),
	}

	c.torrents.Store(t.Id, ct)
	c.torrentsNames.Store(t.Name, t.Id)

	go func() {
		if err := c.SaveTorrent(ct); err != nil {
			_logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) RefreshTorrent(torrentId string) *CachedTorrent {
	_logger := getLogger()

	t, err := c.client.GetTorrent(torrentId)
	if err != nil {
		_logger.Debug().Msgf("Failed to get torrent files for %s: %v", torrentId, err)
		return nil
	}
	if len(t.Files) == 0 {
		return nil
	}

	ct := &CachedTorrent{
		Torrent:       t,
		LastRead:      time.Now(),
		IsComplete:    len(t.Files) > 0,
		DownloadLinks: make(map[string]DownloadLinkCache),
	}

	c.torrents.Store(t.Id, ct)
	c.torrentsNames.Store(t.Name, t.Id)

	go func() {
		if err := c.SaveTorrent(ct); err != nil {
			_logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()

	return ct
}

func (c *Cache) GetFileDownloadLink(t *CachedTorrent, file *torrent.File) (string, error) {
	_logger := getLogger()

	if linkCache, ok := t.DownloadLinks[file.Id]; ok {
		return linkCache.Link, nil
	}

	if file.Link == "" {
		t = c.RefreshTorrent(t.Id)
		if t == nil {
			return "", fmt.Errorf("torrent not found")
		}
		file = t.Torrent.GetFile(file.Id)
	}

	_logger.Debug().Msgf("Getting download link for %s", t.Name)
	link := c.client.GetDownloadLink(t.Torrent, file)
	if link == nil {
		return "", fmt.Errorf("download link not found")
	}

	t.DownloadLinks[file.Id] = DownloadLinkCache{
		Link: link.DownloadLink,
	}

	go func() {
		if err := c.SaveTorrent(t); err != nil {
			_logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()

	return link.DownloadLink, nil
}

func (c *Cache) GetTorrents() *sync.Map {
	return c.torrents
}
