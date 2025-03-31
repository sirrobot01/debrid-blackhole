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
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type WebDavFolderNaming string

const (
	WebDavUseFileName          WebDavFolderNaming = "filename"
	WebDavUseOriginalName      WebDavFolderNaming = "original"
	WebDavUseFileNameNoExt     WebDavFolderNaming = "filename_no_ext"
	WebDavUseOriginalNameNoExt WebDavFolderNaming = "original_no_ext"
	WebDavUseID                WebDavFolderNaming = "id"
)

type PropfindResponse struct {
	Data        []byte
	GzippedData []byte
	Ts          time.Time
}

type CachedTorrent struct {
	*types.Torrent
	AddedOn    time.Time `json:"added_on"`
	IsComplete bool      `json:"is_complete"`
}

type downloadLinkCache struct {
	Link      string
	ExpiresAt time.Time
}

type RepairType string

const (
	RepairTypeReinsert RepairType = "reinsert"
	RepairTypeDelete   RepairType = "delete"
)

type RepairRequest struct {
	Type      RepairType
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

	saveSemaphore chan struct{}
	ctx           context.Context
}

func New(dc config.Debrid, client types.Client) *Cache {
	cfg := config.GetConfig()
	torrentRefreshInterval, err := time.ParseDuration(dc.TorrentsRefreshInterval)
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
	workers := runtime.NumCPU() * 50
	if dc.Workers > 0 {
		workers = dc.Workers
	}
	return &Cache{
		dir:                          filepath.Join(cfg.Path, "cache", dc.Name), // path to save cache files
		torrents:                     xsync.NewMapOf[string, *CachedTorrent](),
		torrentsNames:                xsync.NewMapOf[string, *CachedTorrent](),
		client:                       client,
		logger:                       logger.NewLogger(fmt.Sprintf("%s-webdav", client.GetName())),
		workers:                      workers,
		downloadLinks:                xsync.NewMapOf[string, downloadLinkCache](),
		torrentRefreshInterval:       torrentRefreshInterval,
		downloadLinksRefreshInterval: downloadLinksRefreshInterval,
		PropfindResp:                 xsync.NewMapOf[string, PropfindResponse](),
		folderNaming:                 WebDavFolderNaming(dc.FolderNaming),
		autoExpiresLinksAfter:        autoExpiresLinksAfter,
		repairsInProgress:            xsync.NewMapOf[string, bool](),
		saveSemaphore:                make(chan struct{}, 10),
		ctx:                          context.Background(),
	}
}

func (c *Cache) Start(ctx context.Context) error {
	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}
	c.ctx = ctx

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

func (c *Cache) GetTorrentFolder(torrent *types.Torrent) string {
	switch c.folderNaming {
	case WebDavUseFileName:
		return torrent.Filename
	case WebDavUseOriginalName:
		return torrent.OriginalFilename
	case WebDavUseFileNameNoExt:
		return utils.RemoveExtension(torrent.Filename)
	case WebDavUseOriginalNameNoExt:
		return utils.RemoveExtension(torrent.OriginalFilename)
	case WebDavUseID:
		return torrent.Id
	default:
		return torrent.Filename
	}
}

func (c *Cache) setTorrent(t *CachedTorrent) {
	c.torrents.Store(t.Id, t)

	c.torrentsNames.Store(c.GetTorrentFolder(t.Torrent), t)

	c.SaveTorrent(t)
}

func (c *Cache) setTorrents(torrents map[string]*CachedTorrent) {
	for _, t := range torrents {
		c.torrents.Store(t.Id, t)
		c.torrentsNames.Store(c.GetTorrentFolder(t.Torrent), t)
	}

	c.refreshListings()

	c.SaveTorrents()
}

func (c *Cache) GetListing() []os.FileInfo {
	if v, ok := c.listings.Load().([]os.FileInfo); ok {
		return v
	}
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

	now := time.Now()
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
		isComplete := true
		if len(ct.Files) != 0 {
			// We can assume the torrent is complete

			for _, f := range ct.Files {
				if f.Link == "" {
					c.logger.Debug().Msgf("Torrent %s is not complete, missing link for file %s", ct.Id, f.Name)
					isComplete = false
					continue
				}
			}
			if isComplete {
				addedOn, err := time.Parse(time.RFC3339, ct.Added)
				if err != nil {
					addedOn = now
				}
				ct.AddedOn = addedOn
				ct.IsComplete = true
				torrents[ct.Id] = &ct
			}

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

func (c *Cache) SaveTorrents() {
	c.torrents.Range(func(key string, value *CachedTorrent) bool {
		c.SaveTorrent(value)
		return true
	})
}

func (c *Cache) SaveTorrent(ct *CachedTorrent) {
	// Try to acquire semaphore without blocking
	select {
	case c.saveSemaphore <- struct{}{}:
		go func() {
			defer func() { <-c.saveSemaphore }()
			c.saveTorrent(ct)
		}()
	default:
		c.saveTorrent(ct)
	}
}

func (c *Cache) saveTorrent(ct *CachedTorrent) {
	data, err := json.MarshalIndent(ct, "", "  ")
	if err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to marshal torrent: %s", ct.Id)
		return
	}

	fileName := ct.Torrent.Id + ".json"
	filePath := filepath.Join(c.dir, fileName)

	// Use a unique temporary filename for concurrent safety
	tmpFile := filePath + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 10)

	f, err := os.Create(tmpFile)
	if err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to create file: %s", tmpFile)
		return
	}

	// Track if we've closed the file
	fileClosed := false
	defer func() {
		// Only close if not already closed
		if !fileClosed {
			_ = f.Close()
		}
		// Clean up the temp file if it still exists and rename failed
		_ = os.Remove(tmpFile)
	}()

	w := bufio.NewWriter(f)
	if _, err := w.Write(data); err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to write data: %s", tmpFile)
		return
	}

	if err := w.Flush(); err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to flush data: %s", tmpFile)
		return
	}

	// Close the file before renaming
	_ = f.Close()
	fileClosed = true

	if err := os.Rename(tmpFile, filePath); err != nil {
		c.logger.Debug().Err(err).Msgf("Failed to rename file: %s", tmpFile)
		return
	}
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

	// Create channels with appropriate buffering
	workChan := make(chan *types.Torrent, min(1000, len(torrents)))

	// Use an atomic counter for progress tracking
	var processed int64
	var errorCount int64

	// Create a wait group for workers
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < c.workers; i++ {
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

				case <-c.ctx.Done():
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
		case <-c.ctx.Done():
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
	// Validate each file in the torrent
	for _, file := range t.Files {
		if file.Link == "" {
			c.logger.Debug().Msgf("Torrent %s is not complete, missing link for file %s. Triggering a reinsert", t.Id, file.Name)
			if err := c.ReInsertTorrent(t); err != nil {
				c.logger.Error().Err(err).Msgf("Failed to reinsert torrent %s", t.Id)
				return fmt.Errorf("failed to reinsert torrent: %w", err)
			}
		}
	}

	addedOn, err := time.Parse(time.RFC3339, t.Added)
	if err != nil {
		addedOn = time.Now()
	}
	ct := &CachedTorrent{
		Torrent:    t,
		IsComplete: len(t.Files) > 0,
		AddedOn:    addedOn,
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
			// This code is commented iut due to the fact that if a torrent link is uncached, it's likely that we can't redownload it again
			// Do not attempt to repair the torrent if the hoster is unavailable
			// Check link here??
			//c.logger.Debug().Err(err).Msgf("Hoster is unavailable. Triggering repair for %s", ct.Name)
			//if err := c.repairTorrent(ct); err != nil {
			//	c.logger.Error().Err(err).Msgf("Failed to trigger repair for %s", ct.Name)
			//	return ""
			//}
			//// Generate download link for the file then
			//f := ct.Files[filename]
			//downloadLink, _ = c.client.GetDownloadLink(ct.Torrent, &f)
			//f.DownloadLink = downloadLink
			//file.Generated = time.Now()
			//ct.Files[filename] = f
			//c.updateDownloadLink(file.Link, downloadLink)
			//
			//go func() {
			//	go c.setTorrent(ct)
			//}()
			//
			//return downloadLink // Gets download link in the next pass
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

	c.SaveTorrent(t)
}

func (c *Cache) AddTorrent(t *types.Torrent) error {
	if len(t.Files) == 0 {
		if err := c.client.UpdateTorrent(t); err != nil {
			return fmt.Errorf("failed to update torrent: %w", err)
		}
	}
	addedOn, err := time.Parse(time.RFC3339, t.Added)
	if err != nil {
		addedOn = time.Now()
	}
	ct := &CachedTorrent{
		Torrent:    t,
		IsComplete: len(t.Files) > 0,
		AddedOn:    addedOn,
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

func (c *Cache) DeleteTorrent(id string) error {
	c.logger.Info().Msgf("Deleting torrent %s", id)

	if t, ok := c.torrents.Load(id); ok {
		_ = c.client.DeleteTorrent(id) // SKip error handling, we don't care if it fails
		c.torrents.Delete(id)
		c.torrentsNames.Delete(c.GetTorrentFolder(t.Torrent))
		c.removeFromDB(id)
		c.refreshListings()
	}
	return nil
}

func (c *Cache) DeleteTorrents(ids []string) {
	c.logger.Info().Msgf("Deleting %d torrents", len(ids))
	for _, id := range ids {
		if t, ok := c.torrents.Load(id); ok {
			c.torrents.Delete(id)
			c.torrentsNames.Delete(c.GetTorrentFolder(t.Torrent))
			c.removeFromDB(id)
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
	err := c.DeleteTorrent(torrentId)
	if err != nil {
		c.logger.Error().Err(err).Msgf("Failed to delete torrent: %s", torrentId)
		return
	}
}

func (c *Cache) GetLogger() zerolog.Logger {
	return c.logger
}
