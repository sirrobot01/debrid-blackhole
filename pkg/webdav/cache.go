package webdav

import (
	"bufio"
	"context"
	"fmt"
	"github.com/dgraph-io/badger/v4"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

type DownloadLinkCache struct {
	Link string `json:"download_link"`
}

type propfindResponse struct {
	data []byte
	ts   time.Time
}

type CachedTorrent struct {
	*torrent.Torrent
	LastRead   time.Time `json:"last_read"`
	IsComplete bool      `json:"is_complete"`
}

type Cache struct {
	dir    string
	client debrid.Client
	db     *badger.DB
	logger zerolog.Logger

	torrents      map[string]*CachedTorrent // key: torrent.Id, value: *CachedTorrent
	torrentsNames map[string]*CachedTorrent // key: torrent.Name, value: torrent
	listings      atomic.Value
	downloadLinks map[string]string // key: file.Link, value: download link
	propfindResp  sync.Map

	workers int

	LastUpdated time.Time `json:"last_updated"`

	// refresh mutex
	listingRefreshMu       sync.Mutex // for refreshing torrents
	downloadLinksRefreshMu sync.Mutex // for refreshing download links
	torrentsRefreshMu      sync.Mutex // for refreshing torrents

	// Data Mutexes
	torrentsMutex      sync.RWMutex // for torrents and torrentsNames
	downloadLinksMutex sync.Mutex   // for downloadLinks
}

func (c *Cache) setTorrent(t *CachedTorrent) {
	c.torrentsMutex.Lock()
	c.torrents[t.Id] = t
	c.torrentsNames[t.Name] = t
	c.torrentsMutex.Unlock()

	go c.refreshListings() // This is concurrent safe

	go func() {
		if err := c.SaveTorrent(t); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) refreshListings() {
	// Copy the current torrents to avoid concurrent issues
	c.torrentsMutex.RLock()
	torrents := make([]string, 0, len(c.torrents))
	for _, t := range c.torrents {
		if t != nil && t.Torrent != nil {
			torrents = append(torrents, t.Name)
		}
	}
	c.torrentsMutex.RUnlock()

	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i] < torrents[j]
	})

	files := make([]os.FileInfo, 0, len(torrents))
	now := time.Now()
	for _, t := range torrents {
		files = append(files, &FileInfo{
			name:    t,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: now,
			isDir:   true,
		})
	}
	// Atomic store of the complete ready-to-use slice
	c.listings.Store(files)
}

func (c *Cache) GetListing() []os.FileInfo {
	return c.listings.Load().([]os.FileInfo)
}

func (c *Cache) setTorrents(torrents map[string]*CachedTorrent) {
	c.torrentsMutex.Lock()
	for _, t := range torrents {
		c.torrents[t.Id] = t
		c.torrentsNames[t.Name] = t
	}

	c.torrentsMutex.Unlock()

	go c.refreshListings() // This is concurrent safe

	go func() {
		if err := c.SaveTorrents(); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrents")
		}
	}()
}

func (c *Cache) GetTorrents() map[string]*CachedTorrent {
	c.torrentsMutex.RLock()
	defer c.torrentsMutex.RUnlock()
	result := make(map[string]*CachedTorrent, len(c.torrents))
	for k, v := range c.torrents {
		result[k] = v
	}
	return result
}

func (c *Cache) GetTorrentNames() map[string]*CachedTorrent {
	c.torrentsMutex.RLock()
	defer c.torrentsMutex.RUnlock()
	return c.torrentsNames
}

type Manager struct {
	caches map[string]*Cache
}

func NewCacheManager(clients []debrid.Client) *Manager {
	m := &Manager{
		caches: make(map[string]*Cache),
	}

	for _, client := range clients {
		m.caches[client.GetName()] = NewCache(client)
	}

	return m
}

func (m *Manager) GetCaches() map[string]*Cache {
	return m.caches
}

func (m *Manager) GetCache(debridName string) *Cache {
	return m.caches[debridName]
}

func NewCache(client debrid.Client) *Cache {
	cfg := config.GetConfig()
	dbPath := filepath.Join(cfg.Path, "cache", client.GetName())
	return &Cache{
		dir:           dbPath,
		torrents:      make(map[string]*CachedTorrent),
		torrentsNames: make(map[string]*CachedTorrent),
		client:        client,
		logger:        logger.NewLogger(fmt.Sprintf("%s-cache", client.GetName())),
		workers:       200,
		downloadLinks: make(map[string]string),
	}
}

func (c *Cache) Start() error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := c.Sync(); err != nil {
		return fmt.Errorf("failed to sync cache: %w", err)
	}

	// initial download links
	go func() {
		// lock download refresh mutex
		c.downloadLinksRefreshMu.Lock()
		defer c.downloadLinksRefreshMu.Unlock()
		// This prevents the download links from being refreshed twice
		c.refreshDownloadLinks()
	}()

	go func() {
		err := c.Refresh()
		if err != nil {
			c.logger.Error().Err(err).Msg("Failed to start cache refresh worker")
		}
	}()

	return nil
}

func (c *Cache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *Cache) load() (map[string]*CachedTorrent, error) {
	torrents := make(map[string]*CachedTorrent)
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return torrents, fmt.Errorf("failed to create cache directory: %w", err)
	}

	files, err := os.ReadDir(c.dir)
	if err != nil {
		return torrents, fmt.Errorf("failed to read cache directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(c.dir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to read file: %s", filePath)
			continue
		}

		var ct CachedTorrent
		if err := json.Unmarshal(data, &ct); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to unmarshal file: %s", filePath)
			continue
		}
		if len(ct.Files) != 0 {
			// We can assume the torrent is complete
			ct.IsComplete = true
			torrents[ct.Id] = &ct
		}
	}

	return torrents, nil
}

func (c *Cache) GetTorrent(id string) *CachedTorrent {
	c.torrentsMutex.RLock()
	defer c.torrentsMutex.RUnlock()
	if t, ok := c.torrents[id]; ok {
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

func (c *Cache) SaveTorrents() error {
	for _, ct := range c.GetTorrents() {
		if err := c.SaveTorrent(ct); err != nil {
			return err
		}
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

func (c *Cache) Sync() error {
	cachedTorrents, err := c.load()
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to load cache")
	}

	torrents, err := c.client.GetTorrents()
	if err != nil {
		return fmt.Errorf("failed to sync torrents: %v", err)
	}

	c.logger.Info().Msgf("Got %d torrents from %s", len(torrents), c.client.GetName())

	newTorrents := make([]*torrent.Torrent, 0)
	idStore := make(map[string]bool, len(torrents))
	for _, t := range torrents {
		idStore[t.Id] = true
		if _, ok := cachedTorrents[t.Id]; !ok {
			newTorrents = append(newTorrents, t)
		}
	}

	// Check for deleted torrents
	deletedTorrents := make([]string, 0)
	for _, t := range cachedTorrents {
		if _, ok := idStore[t.Id]; !ok {
			deletedTorrents = append(deletedTorrents, t.Id)
		}
	}

	if len(deletedTorrents) > 0 {
		c.logger.Info().Msgf("Found %d deleted torrents", len(deletedTorrents))
		for _, id := range deletedTorrents {
			if _, ok := cachedTorrents[id]; ok {
				delete(cachedTorrents, id)
				c.removeFromDB(id)
			}
		}
	}

	// Write these torrents to the cache
	c.setTorrents(cachedTorrents)
	c.logger.Info().Msgf("Loaded %d torrents from cache", len(cachedTorrents))

	if len(newTorrents) > 0 {
		c.logger.Info().Msgf("Found %d new torrents", len(newTorrents))
		if err := c.sync(newTorrents); err != nil {
			return fmt.Errorf("failed to sync torrents: %v", err)
		}
	}

	return nil
}

func (c *Cache) sync(torrents []*torrent.Torrent) error {
	// Calculate optimal workers - balance between CPU and IO
	workers := runtime.NumCPU() * 50 // A more balanced multiplier for BadgerDB

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
						c.logger.Error().Err(err).Str("torrent", t.Name).Msg("sync error")
						atomic.AddInt64(&errorCount, 1)
					}

					count := atomic.AddInt64(&processed, 1)
					if count%1000 == 0 {
						c.logger.Info().Msgf("Progress: %d/%d torrents processed", count, len(torrents))
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

	c.logger.Info().Msgf("Sync complete: %d torrents processed, %d errors", len(torrents), errorCount)
	return nil
}

func (c *Cache) processTorrent(t *torrent.Torrent) error {
	var err error
	err = c.client.UpdateTorrent(t)
	if err != nil {
		return fmt.Errorf("failed to get torrent files: %v", err)
	}

	ct := &CachedTorrent{
		Torrent:    t,
		LastRead:   time.Now(),
		IsComplete: len(t.Files) > 0,
	}
	c.setTorrent(ct)
	return nil
}

func (c *Cache) GetDownloadLink(torrentId, filename, fileLink string) string {

	// Check link cache
	if dl := c.checkDownloadLink(fileLink); dl != "" {
		return dl
	}

	ct := c.GetTorrent(torrentId)
	if ct == nil {
		return ""
	}
	file := ct.Files[filename]

	if file.Link == "" {
		// file link is empty, refresh the torrent to get restricted links
		if ct.IsComplete {
			return ""
		}
		ct = c.refreshTorrent(ct) // Refresh the torrent from the debrid service
		if ct == nil {
			return ""
		} else {
			file = ct.Files[filename]
		}
	}

	c.logger.Debug().Msgf("Getting download link for %s", ct.Name)
	f := c.client.GetDownloadLink(ct.Torrent, &file)
	if f == nil {
		return ""
	}
	file.DownloadLink = f.DownloadLink
	ct.Files[filename] = file

	go c.updateDownloadLink(f)
	go c.setTorrent(ct)
	return f.DownloadLink
}

func (c *Cache) updateDownloadLink(file *torrent.File) {
	c.downloadLinksMutex.Lock()
	defer c.downloadLinksMutex.Unlock()
	c.downloadLinks[file.Link] = file.DownloadLink
}

func (c *Cache) checkDownloadLink(link string) string {
	if dl, ok := c.downloadLinks[link]; ok {
		return dl
	}
	return ""
}

func (c *Cache) refreshTorrent(t *CachedTorrent) *CachedTorrent {
	_torrent := t.Torrent
	err := c.client.UpdateTorrent(_torrent)
	if err != nil {
		c.logger.Debug().Msgf("Failed to get torrent files for %s: %v", t.Id, err)
		return nil
	}
	if len(t.Files) == 0 {
		return nil
	}

	ct := &CachedTorrent{
		Torrent:    _torrent,
		LastRead:   time.Now(),
		IsComplete: len(t.Files) > 0,
	}
	c.setTorrent(ct)

	return ct
}

func (c *Cache) refreshDownloadLinks() map[string]string {
	c.downloadLinksMutex.Lock()
	defer c.downloadLinksMutex.Unlock()

	downloadLinks, err := c.client.GetDownloads()
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to get download links")
		return nil
	}
	for k, v := range downloadLinks {
		c.downloadLinks[k] = v.DownloadLink
	}
	return c.downloadLinks
}

func (c *Cache) GetClient() debrid.Client {
	return c.client
}

func (c *Cache) refreshTorrents() {
	c.torrentsMutex.RLock()
	currentTorrents := c.torrents //
	// Create a copy of the current torrents to avoid concurrent issues
	torrents := make(map[string]string, len(currentTorrents)) // a mpa of id and name
	for _, v := range currentTorrents {
		torrents[v.Id] = v.Name
	}
	c.torrentsMutex.RUnlock()

	// Get new torrents from the debrid service
	debTorrents, err := c.client.GetTorrents()
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to get torrents")
		return
	}

	if len(debTorrents) == 0 {
		// Maybe an error occurred
		return
	}

	// Get the newly added torrents only
	newTorrents := make([]*torrent.Torrent, 0)
	idStore := make(map[string]bool, len(debTorrents))
	for _, t := range debTorrents {
		idStore[t.Id] = true
		if _, ok := torrents[t.Id]; !ok {
			newTorrents = append(newTorrents, t)
		}
	}

	// Check for deleted torrents
	deletedTorrents := make([]string, 0)
	for id, _ := range torrents {
		if _, ok := idStore[id]; !ok {
			deletedTorrents = append(deletedTorrents, id)
		}
	}

	if len(deletedTorrents) > 0 {
		c.DeleteTorrent(deletedTorrents)
	}

	if len(newTorrents) == 0 {
		return
	}
	c.logger.Info().Msgf("Found %d new torrents", len(newTorrents))

	// No need for a complex sync process, just add the new torrents
	wg := sync.WaitGroup{}
	wg.Add(len(newTorrents))
	for _, t := range newTorrents {
		// processTorrent is concurrent safe
		go func() {
			defer wg.Done()
			if err := c.processTorrent(t); err != nil {
				c.logger.Info().Err(err).Msg("Failed to process torrent")
			}

		}()
	}
	wg.Wait()
}

func (c *Cache) DeleteTorrent(ids []string) {
	c.logger.Info().Msgf("Deleting %d torrents", len(ids))
	c.torrentsMutex.Lock()
	defer c.torrentsMutex.Unlock()
	for _, id := range ids {
		if t, ok := c.torrents[id]; ok {
			delete(c.torrents, id)
			delete(c.torrentsNames, t.Name)
			c.removeFromDB(id)
		}
	}
}

func (c *Cache) removeFromDB(torrentId string) {
	filePath := filepath.Join(c.dir, torrentId+".json")
	if err := os.Remove(filePath); err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to remove file: %s", filePath)
	}
}

func (c *Cache) resetPropfindResponse() {
	// Right now, parents are hardcoded
	parents := []string{"__all__", "torrents"}
	// Reset only the parent directories
	// Convert the parents to a keys
	// This is a bit hacky, but it works
	// Instead of deleting all the keys, we only delete the parent keys, e.g __all__/ or torrents/
	keys := make([]string, 0, len(parents))
	for _, p := range parents {
		// Construct the key
		// construct url
		url := filepath.Join("/webdav/%s/%s", c.client.GetName(), p)
		key0 := fmt.Sprintf("propfind:%s:0", url)
		key1 := fmt.Sprintf("propfind:%s:1", url)
		keys = append(keys, key0, key1)
	}

	// Delete the keys
	for _, k := range keys {
		c.propfindResp.Delete(k)
	}
}
