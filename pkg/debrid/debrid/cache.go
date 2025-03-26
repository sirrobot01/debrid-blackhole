package debrid

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
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

type downloadLinkCache struct {
	Link      string
	ExpiresAt time.Time
}

type RepairRequest struct {
	TorrentID string
	Priority  int
	FileName  string
}

type Cache struct {
	dir    string
	client types.Client
	logger zerolog.Logger

	torrents      *xsync.MapOf[string, *CachedTorrent] // key: torrent.Id, value: *CachedTorrent
	torrentsNames *xsync.MapOf[string, *CachedTorrent] // key: torrent.Name, value: torrent
	listings      atomic.Value
	downloadLinks *xsync.MapOf[string, downloadLinkCache]
	PropfindResp  *xsync.MapOf[string, PropfindResponse]
	folderNaming  WebDavFolderNaming

	// repair
	repairChan        chan RepairRequest
	repairsInProgress *xsync.MapOf[string, bool]

	// config
	workers                      int
	torrentRefreshInterval       time.Duration
	downloadLinksRefreshInterval time.Duration
	autoExpiresLinksAfter        time.Duration

	// refresh mutex
	listingRefreshMu       sync.RWMutex // for refreshing torrents
	downloadLinksRefreshMu sync.RWMutex // for refreshing download links
	torrentsRefreshMu      sync.RWMutex // for refreshing torrents
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
	autoExpiresLinksAfter, err := time.ParseDuration(dc.AutoExpireLinksAfter)
	if err != nil {
		autoExpiresLinksAfter = time.Hour * 24
	}
	return &Cache{
		dir:                          filepath.Join(cfg.Path, "cache", dc.Name), // path to save cache files
		torrents:                     xsync.NewMapOf[string, *CachedTorrent](),
		torrentsNames:                xsync.NewMapOf[string, *CachedTorrent](),
		client:                       client,
		logger:                       logger.NewLogger(fmt.Sprintf("%s-cache", client.GetName())),
		workers:                      200,
		downloadLinks:                xsync.NewMapOf[string, downloadLinkCache](),
		torrentRefreshInterval:       torrentRefreshInterval,
		downloadLinksRefreshInterval: downloadLinksRefreshInterval,
		PropfindResp:                 xsync.NewMapOf[string, PropfindResponse](),
		folderNaming:                 WebDavFolderNaming(dc.WebDavFolderNaming),
		autoExpiresLinksAfter:        autoExpiresLinksAfter,
		repairsInProgress:            xsync.NewMapOf[string, bool](),
	}
}

func (c *Cache) GetTorrentFolder(torrent *types.Torrent) string {
	folderName := torrent.Filename
	if c.folderNaming == WebDavUseID {
		folderName = torrent.Id
	} else if c.folderNaming == WebDavUseOriginalNameNoExt {
		folderName = utils.RemoveExtension(folderName)
	}
	return folderName
}

func (c *Cache) setTorrent(t *CachedTorrent) {
	c.torrents.Store(t.Id, t)

	c.torrentsNames.Store(c.GetTorrentFolder(t.Torrent), t)

	go func() {
		if err := c.SaveTorrent(t); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) setTorrents(torrents map[string]*CachedTorrent) {
	for _, t := range torrents {
		c.torrents.Store(t.Id, t)
		c.torrentsNames.Store(c.GetTorrentFolder(t.Torrent), t)
	}

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

func (c *Cache) Start() error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := c.Sync(); err != nil {
		return fmt.Errorf("failed to sync cache: %w", err)
	}

	// initial download links
	go func() {
		c.refreshDownloadLinks()
	}()

	go func() {
		err := c.Refresh()
		if err != nil {
			c.logger.Error().Err(err).Msg("Failed to start cache refresh worker")
		}
	}()

	c.repairChan = make(chan RepairRequest, 100)
	go c.repairWorker()

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

			// Make sure no file has a duplicate link
			linkStore := make(map[string]bool)
			for _, f := range ct.Files {
				if _, ok := linkStore[f.Link]; ok {
					// Duplicate link, refresh the torrent
					ct = *c.refreshTorrent(&ct)
					break
				} else {
					linkStore[f.Link] = true
				}
			}

			ct.IsComplete = true
			torrents[ct.Id] = &ct
		}
	}

	return torrents, nil
}

func (c *Cache) GetTorrents() map[string]*CachedTorrent {
	torrents := make(map[string]*CachedTorrent)
	c.torrents.Range(func(key string, value *CachedTorrent) bool {
		torrents[key] = value
		return true
	})
	return torrents
}

func (c *Cache) GetTorrent(id string) *CachedTorrent {
	if t, ok := c.torrents.Load(id); ok {
		return t
	}
	return nil
}

func (c *Cache) GetTorrentByName(name string) *CachedTorrent {
	if t, ok := c.torrentsNames.Load(name); ok {
		return t
	}
	return nil
}

func (c *Cache) SaveTorrents() error {
	c.torrents.Range(func(key string, value *CachedTorrent) bool {
		if err := c.SaveTorrent(value); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrent %s", key)
		}
		return true
	})
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

					if err := c.ProcessTorrent(t, false); err != nil {
						c.logger.Error().Err(err).Str("torrent", t.Name).Msg("sync error")
						atomic.AddInt64(&errorCount, 1)
					}

					count := atomic.AddInt64(&processed, 1)
					if count%1000 == 0 {
						c.refreshListings()
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

	c.refreshListings()
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

	if refreshRclone {
		c.refreshListings()
	}
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
		ct = c.refreshTorrent(ct) // Refresh the torrent from the debrid
		if ct == nil {
			return ""
		} else {
			file = ct.Files[filename]
		}
	}

	c.logger.Trace().Msgf("Getting download link for %s", filename)
	downloadLink, err := c.client.GetDownloadLink(ct.Torrent, &file)
	if err != nil {
		if errors.Is(err, request.HosterUnavailableError) {
			// Check link here??
			c.logger.Debug().Err(err).Msgf("Hoster is unavailable. Triggering repair for %s", ct.Name)
			if err := c.repairTorrent(ct); err != nil {
				c.logger.Error().Err(err).Msgf("Failed to trigger repair for %s", ct.Name)
				return ""
			}
			// Generate download link for the file then
			f := ct.Files[filename]
			downloadLink, _ = c.client.GetDownloadLink(ct.Torrent, &f)
			f.DownloadLink = downloadLink
			file.Generated = time.Now()
			ct.Files[filename] = f
			c.updateDownloadLink(file.Link, downloadLink)

			go func() {
				go c.setTorrent(ct)
			}()

			return downloadLink // Gets download link in the next pass
		}

		c.logger.Debug().Err(err).Msgf("Failed to get download link for :%s", file.Link)
		return ""
	}
	file.DownloadLink = downloadLink
	file.Generated = time.Now()
	ct.Files[filename] = file

	go c.updateDownloadLink(file.Link, downloadLink)
	go c.setTorrent(ct)
	return file.DownloadLink
}

func (c *Cache) GenerateDownloadLinks(t *CachedTorrent) {
	if err := c.client.GenerateDownloadLinks(t.Torrent); err != nil {
		c.logger.Error().Err(err).Msg("Failed to generate download links")
	}
	for _, file := range t.Files {
		c.updateDownloadLink(file.Link, file.DownloadLink)
	}

	go func() {
		if err := c.SaveTorrent(t); err != nil {
			c.logger.Debug().Err(err).Msgf("Failed to save torrent %s", t.Id)
		}
	}()
}

func (c *Cache) AddTorrent(t *types.Torrent) error {
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
	c.refreshListings()
	go c.GenerateDownloadLinks(ct)
	return nil

}

func (c *Cache) updateDownloadLink(link, downloadLink string) {
	c.downloadLinks.Store(link, downloadLinkCache{
		Link:      downloadLink,
		ExpiresAt: time.Now().Add(c.autoExpiresLinksAfter), // Expires in 24 hours
	})
}

func (c *Cache) checkDownloadLink(link string) string {
	if dl, ok := c.downloadLinks.Load(link); ok {
		if dl.ExpiresAt.After(time.Now()) {
			return dl.Link
		}
	}
	return ""
}

func (c *Cache) GetClient() types.Client {
	return c.client
}

func (c *Cache) DeleteTorrent(id string) {
	c.logger.Info().Msgf("Deleting torrent %s", id)

	if t, ok := c.torrents.Load(id); ok {
		c.torrents.Delete(id)
		c.torrentsNames.Delete(c.GetTorrentFolder(t.Torrent))
		go c.removeFromDB(id)
		c.refreshListings()
	}
}

func (c *Cache) DeleteTorrents(ids []string) {
	c.logger.Info().Msgf("Deleting %d torrents", len(ids))
	for _, id := range ids {
		if t, ok := c.torrents.Load(id); ok {
			c.torrents.Delete(id)
			c.torrentsNames.Delete(c.GetTorrentFolder(t.Torrent))
			go c.removeFromDB(id)
		}
	}
	c.refreshListings()
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
