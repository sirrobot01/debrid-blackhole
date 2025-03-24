package debrid

import (
	"bufio"
	"context"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type WebDavFolderNaming string

const (
	WebDavUseOriginalName      WebDavFolderNaming = "original"
	WebDavUseID                WebDavFolderNaming = "use_id"
	WebDavUseOriginalNameNoExt WebDavFolderNaming = "original_no_ext"
)

type DownloadLinkCache struct {
	Link string `json:"download_link"`
}

type PropfindResponse struct {
	Data        []byte
	GzippedData []byte
	Ts          time.Time
}

type CachedTorrent struct {
	*types.Torrent
	LastRead   time.Time `json:"last_read"`
	IsComplete bool      `json:"is_complete"`
}

type Cache struct {
	dir    string
	client types.Client
	logger zerolog.Logger

	torrents      map[string]*CachedTorrent // key: torrent.Id, value: *CachedTorrent
	torrentsNames map[string]*CachedTorrent // key: torrent.Name, value: torrent
	listings      atomic.Value
	downloadLinks map[string]string // key: file.Link, value: download link
	PropfindResp  *xsync.MapOf[string, PropfindResponse]
	folderNaming  WebDavFolderNaming

	// config
	workers                      int
	torrentRefreshInterval       time.Duration
	downloadLinksRefreshInterval time.Duration

	// refresh mutex
	listingRefreshMu       sync.RWMutex // for refreshing torrents
	downloadLinksRefreshMu sync.RWMutex // for refreshing download links
	torrentsRefreshMu      sync.RWMutex // for refreshing torrents

	// Data Mutexes
	torrentsMutex      sync.RWMutex // for torrents and torrentsNames
	downloadLinksMutex sync.Mutex   // for downloadLinks
}

func NewCache(dc config.Debrid, client types.Client) *Cache {
	cfg := config.GetConfig()
	torrentRefreshInterval, err := time.ParseDuration(dc.TorrentRefreshInterval)
	if err != nil {
		torrentRefreshInterval = time.Second * 15
	}
	downloadLinksRefreshInterval, err := time.ParseDuration(dc.DownloadLinksRefreshInterval)
	if err != nil {
		downloadLinksRefreshInterval = time.Minute * 40
	}
	return &Cache{
		dir:                          filepath.Join(cfg.Path, "cache", dc.Name), // path to save cache files
		torrents:                     make(map[string]*CachedTorrent),
		torrentsNames:                make(map[string]*CachedTorrent),
		client:                       client,
		logger:                       logger.NewLogger(fmt.Sprintf("%s-cache", client.GetName())),
		workers:                      200,
		downloadLinks:                make(map[string]string),
		torrentRefreshInterval:       torrentRefreshInterval,
		downloadLinksRefreshInterval: downloadLinksRefreshInterval,
		PropfindResp:                 xsync.NewMapOf[string, PropfindResponse](),
		folderNaming:                 WebDavFolderNaming(dc.WebDavFolderNaming),
	}
}

func (c *Cache) GetTorrentFolder(torrent *types.Torrent) string {
	folderName := torrent.Name
	if c.folderNaming == WebDavUseID {
		folderName = torrent.Id
	} else if c.folderNaming == WebDavUseOriginalNameNoExt {
		folderName = utils.RemoveExtension(torrent.Name)
	}
	return folderName
}

func (c *Cache) setTorrent(t *CachedTorrent) {
	c.torrentsMutex.Lock()
	c.torrents[t.Id] = t

	c.torrentsNames[c.GetTorrentFolder(t.Torrent)] = t
	c.torrentsMutex.Unlock()

	c.refreshListings()

	go func() {
		if err := c.SaveTorrent(t); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) setTorrents(torrents map[string]*CachedTorrent) {
	c.torrentsMutex.Lock()
	for _, t := range torrents {
		c.torrents[t.Id] = t
		c.torrentsNames[c.GetTorrentFolder(t.Torrent)] = t
	}

	c.torrentsMutex.Unlock()

	c.refreshListings()

	go func() {
		if err := c.SaveTorrents(); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrents")
		}
	}()
}

func (c *Cache) GetListing() []os.FileInfo {
	if v, ok := c.listings.Load().([]os.FileInfo); ok {
		return v
	}
	return nil
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

	newTorrents := make([]*types.Torrent, 0)
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

func (c *Cache) sync(torrents []*types.Torrent) error {
	// Calculate optimal workers - balance between CPU and IO
	workers := runtime.NumCPU() * 50 // A more balanced multiplier for BadgerDB

	// Create channels with appropriate buffering
	workChan := make(chan *types.Torrent, workers*2)

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

					if err := c.ProcessTorrent(t, true); err != nil {
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

func (c *Cache) ProcessTorrent(t *types.Torrent, refreshRclone bool) error {
	if len(t.Files) == 0 {
		if err := c.client.UpdateTorrent(t); err != nil {
			return fmt.Errorf("failed to update torrent: %w", err)
		}
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
		ct = c.refreshTorrent(ct) // Refresh the torrent from the debrid
		if ct == nil {
			return ""
		} else {
			file = ct.Files[filename]
		}
	}

	c.logger.Trace().Msgf("Getting download link for %s", ct.Name)
	link, err := c.client.GetDownloadLink(ct.Torrent, &file)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get download link")
		return ""
	}
	file.DownloadLink = link
	file.Generated = time.Now()
	ct.Files[filename] = file

	go c.updateDownloadLink(file)
	go c.setTorrent(ct)
	return file.DownloadLink
}

func (c *Cache) updateDownloadLink(file types.File) {
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

func (c *Cache) GetClient() types.Client {
	return c.client
}

func (c *Cache) DeleteTorrent(id string) {
	c.logger.Info().Msgf("Deleting torrent %s", id)
	c.torrentsMutex.Lock()
	defer c.torrentsMutex.Unlock()
	if t, ok := c.torrents[id]; ok {
		delete(c.torrents, id)
		delete(c.torrentsNames, t.Name)

		c.removeFromDB(id)
	}
}

func (c *Cache) DeleteTorrents(ids []string) {
	c.logger.Info().Msgf("Deleting %d torrents", len(ids))
	c.torrentsMutex.Lock()
	defer c.torrentsMutex.Unlock()
	for _, id := range ids {
		if t, ok := c.torrents[id]; ok {
			delete(c.torrents, id)
			delete(c.torrentsNames, c.GetTorrentFolder(t.Torrent))
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

func (c *Cache) OnRemove(torrentId string) {
	c.logger.Debug().Msgf("OnRemove triggered for %s", torrentId)
	go c.DeleteTorrent(torrentId)
	go c.refreshListings()
}
