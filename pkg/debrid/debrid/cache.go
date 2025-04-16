package debrid

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"os"
	"path/filepath"
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
	Id        string
	Link      string
	AccountId string
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

	torrents             *xsync.MapOf[string, *CachedTorrent] // key: torrent.Id, value: *CachedTorrent
	torrentsNames        *xsync.MapOf[string, *CachedTorrent] // key: torrent.Name, value: torrent
	listings             atomic.Value
	downloadLinks        *xsync.MapOf[string, downloadLinkCache]
	invalidDownloadLinks *xsync.MapOf[string, string]
	PropfindResp         *xsync.MapOf[string, PropfindResponse]
	folderNaming         WebDavFolderNaming

	// repair
	repairChan        chan RepairRequest
	repairsInProgress *xsync.MapOf[string, struct{}]

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
	cfg := config.Get()
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
	return &Cache{
		dir:                          filepath.Join(cfg.Path, "cache", dc.Name), // path to save cache files
		torrents:                     xsync.NewMapOf[string, *CachedTorrent](),
		torrentsNames:                xsync.NewMapOf[string, *CachedTorrent](),
		invalidDownloadLinks:         xsync.NewMapOf[string, string](),
		client:                       client,
		logger:                       logger.New(fmt.Sprintf("%s-webdav", client.GetName())),
		workers:                      dc.Workers,
		downloadLinks:                xsync.NewMapOf[string, downloadLinkCache](),
		torrentRefreshInterval:       torrentRefreshInterval,
		downloadLinksRefreshInterval: downloadLinksRefreshInterval,
		PropfindResp:                 xsync.NewMapOf[string, PropfindResponse](),
		folderNaming:                 WebDavFolderNaming(dc.FolderNaming),
		autoExpiresLinksAfter:        autoExpiresLinksAfter,
		repairsInProgress:            xsync.NewMapOf[string, struct{}](),
		saveSemaphore:                make(chan struct{}, 50),
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

func (c *Cache) load() (map[string]*CachedTorrent, error) {
	torrents := make(map[string]*CachedTorrent)
	var results sync.Map

	if err := os.MkdirAll(c.dir, 0755); err != nil {
		return torrents, fmt.Errorf("failed to create cache directory: %w", err)
	}

	files, err := os.ReadDir(c.dir)
	if err != nil {
		return torrents, fmt.Errorf("failed to read cache directory: %w", err)
	}

	// Get only json files
	var jsonFiles []os.DirEntry
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			jsonFiles = append(jsonFiles, file)
		}
	}

	if len(jsonFiles) == 0 {
		return torrents, nil
	}

	// Create channels with appropriate buffering
	workChan := make(chan os.DirEntry, min(c.workers, len(jsonFiles)))

	// Create a wait group for workers
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			now := time.Now()

			for {
				file, ok := <-workChan
				if !ok {
					return // Channel closed, exit goroutine
				}

				fileName := file.Name()
				filePath := filepath.Join(c.dir, fileName)
				data, err := os.ReadFile(filePath)
				if err != nil {
					c.logger.Error().Err(err).Msgf("Failed to read file: %s", filePath)
					continue
				}

				var ct CachedTorrent
				if err := json.Unmarshal(data, &ct); err != nil {
					c.logger.Error().Err(err).Msgf("Failed to unmarshal file: %s", filePath)
					continue
				}

				isComplete := true
				if len(ct.Files) != 0 {
					// Check if all files are valid, if not, delete the file.json and remove from cache.
					for _, f := range ct.Files {
						if f.Link == "" {
							isComplete = false
							break
						}
					}

					if isComplete {
						addedOn, err := time.Parse(time.RFC3339, ct.Added)
						if err != nil {
							addedOn = now
						}
						ct.AddedOn = addedOn
						ct.IsComplete = true
						results.Store(ct.Id, &ct)
					}
				}
			}
		}()
	}

	// Feed work to workers
	for _, file := range jsonFiles {
		workChan <- file
	}

	// Signal workers that no more work is coming
	close(workChan)

	// Wait for all workers to complete
	wg.Wait()

	// Convert sync.Map to regular map
	results.Range(func(key, value interface{}) bool {
		id, _ := key.(string)
		torrent, _ := value.(*CachedTorrent)
		torrents[id] = torrent
		return true
	})

	return torrents, nil
}

func (c *Cache) Sync() error {
	defer c.logger.Info().Msg("WebDav server sync complete")
	cachedTorrents, err := c.load()
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to load cache")
	}

	torrents, err := c.client.GetTorrents()
	if err != nil {
		return fmt.Errorf("failed to sync torrents: %v", err)
	}

	c.logger.Info().Msgf("Got %d torrents from %s", len(torrents), c.client.GetName())

	newTorrents := make([]*types.Torrent, 0)
	idStore := make(map[string]struct{}, len(torrents))
	for _, t := range torrents {
		idStore[t.Id] = struct{}{}
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
	workChan := make(chan *types.Torrent, min(c.workers, len(torrents)))

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
	marshaled, err := json.MarshalIndent(ct, "", "  ")
	if err != nil {
		c.logger.Error().Err(err).Msgf("Failed to marshal torrent: %s", ct.Id)
		return
	}

	// Store just the essential info needed for the file operation
	saveInfo := struct {
		id       string
		jsonData []byte
	}{
		id:       ct.Torrent.Id,
		jsonData: marshaled,
	}

	// Try to acquire semaphore without blocking
	select {
	case c.saveSemaphore <- struct{}{}:
		go func() {
			defer func() { <-c.saveSemaphore }()
			c.saveTorrent(saveInfo.id, saveInfo.jsonData)
		}()
	default:
		c.saveTorrent(saveInfo.id, saveInfo.jsonData)
	}
}

func (c *Cache) saveTorrent(id string, data []byte) {

	fileName := id + ".json"
	filePath := filepath.Join(c.dir, fileName)

	// Use a unique temporary filename for concurrent safety
	tmpFile := filePath + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 10)

	f, err := os.Create(tmpFile)
	if err != nil {
		c.logger.Error().Err(err).Msgf("Failed to create file: %s", tmpFile)
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
		c.logger.Error().Err(err).Msgf("Failed to write data: %s", tmpFile)
		return
	}

	if err := w.Flush(); err != nil {
		c.logger.Error().Err(err).Msgf("Failed to flush data: %s", tmpFile)
		return
	}

	// Close the file before renaming
	_ = f.Close()
	fileClosed = true

	if err := os.Rename(tmpFile, filePath); err != nil {
		c.logger.Error().Err(err).Msgf("Failed to rename file: %s", tmpFile)
		return
	}
}

func (c *Cache) ProcessTorrent(t *types.Torrent, refreshRclone bool) error {

	isComplete := func(files map[string]types.File) bool {
		_complete := len(files) > 0
		for _, file := range files {
			if file.Link == "" {
				_complete = false
				break
			}
		}
		return _complete
	}

	if !isComplete(t.Files) {
		if err := c.client.UpdateTorrent(t); err != nil {
			return fmt.Errorf("failed to update torrent: %w", err)
		}
	}

	if !isComplete(t.Files) {
		c.logger.Debug().Msgf("Torrent %s is still not complete. Triggering a reinsert(disabled)", t.Id)
		//ct, err := c.reInsertTorrent(t)
		//if err != nil {
		//	c.logger.Error().Err(err).Msgf("Failed to reinsert torrent %s", t.Id)
		//	return err
		//}
		//c.logger.Debug().Msgf("Reinserted torrent %s", ct.Id)

	} else {
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
	}

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

	// If file.Link is still empty, return
	if file.Link == "" {
		c.logger.Debug().Msgf("File link is empty for %s. Release is probably nerfed", filename)
		// Try to reinsert the torrent?
		ct, err := c.reInsertTorrent(ct)
		if err != nil {
			c.logger.Error().Err(err).Msgf("Failed to reinsert torrent %s", ct.Name)
			return ""
		}
		file = ct.Files[filename]
		c.logger.Debug().Msgf("Reinserted torrent %s", ct.Name)
	}

	c.logger.Trace().Msgf("Getting download link for %s", filename)
	downloadLink, err := c.client.GetDownloadLink(ct.Torrent, &file)
	if err != nil {
		if errors.Is(err, request.HosterUnavailableError) {
			c.logger.Error().Err(err).Msgf("Hoster is unavailable. Triggering repair for %s", ct.Name)
			ct, err := c.reInsertTorrent(ct)
			if err != nil {
				c.logger.Error().Err(err).Msgf("Failed to reinsert torrent %s", ct.Name)
				return ""
			}
			c.logger.Debug().Msgf("Reinserted torrent %s", ct.Name)
			file = ct.Files[filename]
			// Retry getting the download link
			downloadLink, err = c.client.GetDownloadLink(ct.Torrent, &file)
			if err != nil {
				c.logger.Error().Err(err).Msgf("Failed to get download link for %s", file.Link)
				return ""
			}
			if downloadLink == nil {
				c.logger.Debug().Msgf("Download link is empty for %s", file.Link)
				return ""
			}
			c.updateDownloadLink(downloadLink)
			return downloadLink.DownloadLink
		} else if errors.Is(err, request.TrafficExceededError) {
			// This is likely a fair usage limit error
			c.logger.Error().Err(err).Msgf("Traffic exceeded for %s", ct.Name)
		} else {
			c.logger.Error().Err(err).Msgf("Failed to get download link for %s", file.Link)
			return ""
		}
	}

	if downloadLink == nil {
		c.logger.Debug().Msgf("Download link is empty for %s", file.Link)
		return ""
	}
	c.updateDownloadLink(downloadLink)
	return downloadLink.DownloadLink
}

func (c *Cache) GenerateDownloadLinks(t *CachedTorrent) {
	if err := c.client.GenerateDownloadLinks(t.Torrent); err != nil {
		c.logger.Error().Err(err).Msg("Failed to generate download links")
	}
	for _, file := range t.Files {
		if file.DownloadLink != nil {
			c.updateDownloadLink(file.DownloadLink)
		}

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

func (c *Cache) updateDownloadLink(dl *types.DownloadLink) {
	c.downloadLinks.Store(dl.Link, downloadLinkCache{
		Id:        dl.Id,
		Link:      dl.DownloadLink,
		ExpiresAt: time.Now().Add(c.autoExpiresLinksAfter),
		AccountId: dl.AccountId,
	})
}

func (c *Cache) checkDownloadLink(link string) string {
	if dl, ok := c.downloadLinks.Load(link); ok {
		if dl.ExpiresAt.After(time.Now()) && !c.IsDownloadLinkInvalid(dl.Link) {
			return dl.Link
		}
	}
	return ""
}

func (c *Cache) MarkDownloadLinkAsInvalid(link, downloadLink, reason string) {
	c.invalidDownloadLinks.Store(downloadLink, reason)
	// Remove the download api key from active
	if reason == "bandwidth_exceeded" {
		if dl, ok := c.downloadLinks.Load(link); ok {
			if dl.AccountId != "" && dl.Link == downloadLink {
				c.client.DisableAccount(dl.AccountId)
			}
		}
	}
	c.removeDownloadLink(link)
}

func (c *Cache) removeDownloadLink(link string) {
	if dl, ok := c.downloadLinks.Load(link); ok {
		// Delete dl from cache
		c.downloadLinks.Delete(link)
		// Delete dl from debrid
		if dl.Id != "" {
			_ = c.client.DeleteDownloadLink(dl.Id)
		}
	}
}

func (c *Cache) IsDownloadLinkInvalid(downloadLink string) bool {
	if reason, ok := c.invalidDownloadLinks.Load(downloadLink); ok {
		c.logger.Debug().Msgf("Download link %s is invalid: %s", downloadLink, reason)
		return true
	}
	return false
}

func (c *Cache) GetClient() types.Client {
	return c.client
}

func (c *Cache) DeleteTorrent(id string) error {
	c.logger.Info().Msgf("Deleting torrent %s", id)
	c.torrentsRefreshMu.Lock()
	defer c.torrentsRefreshMu.Unlock()

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
	// Moves the torrent file to the trash
	filePath := filepath.Join(c.dir, torrentId+".json")

	// Check if the file exists
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		return
	}

	// Move the file to the trash
	trashPath := filepath.Join(c.dir, "trash", torrentId+".json")
	if err := os.MkdirAll(filepath.Dir(trashPath), 0755); err != nil {
		return
	}
	if err := os.Rename(filePath, trashPath); err != nil {
		return
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
