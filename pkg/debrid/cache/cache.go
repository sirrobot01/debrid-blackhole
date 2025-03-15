package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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
		cfg := config.GetConfig()
		_logInstance = logger.NewLogger("cache", cfg.LogLevel, os.Stdout)
	})
	return _logInstance
}

type Cache struct {
	dir               string
	client            engine.Service
	db                *badger.DB
	torrents          map[string]*CachedTorrent // key: torrent.Id, value: *CachedTorrent
	torrentsMutex     sync.RWMutex
	torrentsNames     map[string]*CachedTorrent // key: torrent.Name, value: torrent
	torrentNamesMutex sync.RWMutex
	LastUpdated       time.Time `json:"last_updated"`
}

func (c *Cache) SetTorrent(t *CachedTorrent) {
	c.torrentsMutex.Lock()
	defer c.torrentsMutex.Unlock()
	c.torrents[t.Id] = t
}

func (c *Cache) SetTorrentName(name string, t *CachedTorrent) {
	c.torrentNamesMutex.Lock()
	defer c.torrentNamesMutex.Unlock()
	c.torrentsNames[name] = t
}

func (c *Cache) GetTorrents() map[string]*CachedTorrent {
	c.torrentsMutex.RLock()
	defer c.torrentsMutex.RUnlock()
	return c.torrents
}

func (c *Cache) GetTorrentNames() map[string]*CachedTorrent {
	c.torrentNamesMutex.RLock()
	defer c.torrentNamesMutex.RUnlock()
	return c.torrentsNames
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
	dbPath := filepath.Join(basePath, "cache", debridService.GetName(), "db")
	return &Cache{
		dir:           dbPath,
		torrents:      make(map[string]*CachedTorrent),
		torrentsNames: make(map[string]*CachedTorrent),
		client:        debridService,
	}
}

func (c *Cache) Start() error {
	_logger := getLogger()
	_logger.Info().Msg("Starting cache for: " + c.client.GetName())

	// Make sure the directory exists
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(c.dir)
	opts.Logger = nil // Disable Badger's internal logger

	var err error
	c.db, err = badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open BadgerDB: %w", err)
	}

	if err := c.Load(); err != nil {
		return fmt.Errorf("failed to load cache: %v", err)
	}

	if err := c.Sync(); err != nil {
		return fmt.Errorf("failed to sync cache: %v", err)
	}

	return nil
}

func (c *Cache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *Cache) Load() error {
	_logger := getLogger()

	err := c.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("torrent:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var ct CachedTorrent
				if err := json.Unmarshal(val, &ct); err != nil {
					_logger.Debug().Err(err).Msgf("Failed to unmarshal torrent")
					return nil // Continue to next item
				}

				if len(ct.Files) > 0 {
					c.SetTorrent(&ct)
					c.SetTorrentName(ct.Name, &ct)
				}
				return nil
			})

			if err != nil {
				_logger.Debug().Err(err).Msg("Error reading torrent value")
			}
		}
		return nil
	})

	return err
}

func (c *Cache) GetTorrent(id string) *CachedTorrent {
	if t, ok := c.GetTorrents()[id]; ok {
		return t
	}
	return nil
}

func (c *Cache) GetTorrentByName(name string) *CachedTorrent {
	if t, ok := c.GetTorrentNames()[name]; ok {
		return t
	}
	return nil
}

func (c *Cache) SaveTorrent(ct *CachedTorrent) error {
	data, err := json.Marshal(ct)
	if err != nil {
		return fmt.Errorf("failed to marshal torrent: %w", err)
	}

	key := []byte(fmt.Sprintf("torrent:%s", ct.Torrent.Id))

	err = c.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, data)
	})

	if err != nil {
		return fmt.Errorf("failed to save torrent to BadgerDB: %w", err)
	}

	// Also create an index by name for quick lookups
	nameKey := []byte(fmt.Sprintf("name:%s", ct.Torrent.Name))
	err = c.db.Update(func(txn *badger.Txn) error {
		return txn.Set(nameKey, []byte(ct.Torrent.Id))
	})

	if err != nil {
		return fmt.Errorf("failed to save torrent name index: %w", err)
	}

	return nil
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

	for _, value := range c.GetTorrents() {
		tasks <- value
	}

	close(tasks)
	wg.Wait()
	c.LastUpdated = time.Now()

	// Run value log garbage collection when appropriate
	// This helps reclaim space from deleted/updated values
	go func() {
		err := c.db.RunValueLogGC(0.5) // Run GC if 50% of the value log can be discarded
		if err != nil && err != badger.ErrNoRewrite {
			_logger.Debug().Err(err).Msg("BadgerDB value log GC")
		}
	}()

	return nil
}

func (c *Cache) Sync() error {
	_logger := getLogger()
	torrents, err := c.client.GetTorrents()
	if err != nil {
		return fmt.Errorf("failed to sync torrents: %v", err)
	}
	_logger.Info().Msgf("Syncing %d torrents", len(torrents))

	// Calculate optimal workers - balance between CPU and IO
	workers := runtime.NumCPU() * 4 // A more balanced multiplier for BadgerDB

	// Create channels with appropriate buffering
	workChan := make(chan *torrent.Torrent, workers*2)

	// Use an atomic counter for progress tracking
	var processed int64
	var errorCount int64

	// Create a context with cancellation in case of critical errors
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a wait group for workers
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case t, ok := <-workChan:
					if !ok {
						return // Channel closed, exit goroutine
					}

					if err := c.processTorrent(t); err != nil {
						_logger.Error().Err(err).Str("torrent", t.Name).Msg("sync error")
						atomic.AddInt64(&errorCount, 1)
					}

					count := atomic.AddInt64(&processed, 1)
					if count%1000 == 0 {
						_logger.Info().Msgf("Progress: %d/%d torrents processed", count, len(torrents))
					}

				case <-ctx.Done():
					return // Context cancelled, exit goroutine
				}
			}
		}()
	}

	// Feed work to workers
	for _, t := range torrents {
		select {
		case workChan <- t:
			// Work sent successfully
		case <-ctx.Done():
			break // Context cancelled
		}
	}

	// Signal workers that no more work is coming
	close(workChan)

	// Wait for all workers to complete
	wg.Wait()

	_logger.Info().Msgf("Sync complete: %d torrents processed, %d errors", len(torrents), errorCount)
	return nil
}

func (c *Cache) processTorrent(t *torrent.Torrent) error {
	if ct := c.GetTorrent(t.Id); ct != nil {
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
		tNew, err := c.client.GetTorrent(t)
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

	c.SetTorrent(ct)
	c.SetTorrentName(t.Name, ct)

	go func() {
		if err := c.SaveTorrent(ct); err != nil {
			_logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) RefreshTorrent(torrent *CachedTorrent) *CachedTorrent {
	_logger := getLogger()

	t, err := c.client.GetTorrent(torrent.Torrent)
	if err != nil {
		_logger.Debug().Msgf("Failed to get torrent files for %s: %v", torrent.Id, err)
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

	c.SetTorrent(ct)
	c.SetTorrentName(t.Name, ct)

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
		t = c.RefreshTorrent(t)
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
