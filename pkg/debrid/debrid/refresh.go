package debrid

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.size }
func (fi *fileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *fileInfo) ModTime() time.Time { return fi.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.isDir }
func (fi *fileInfo) Sys() interface{}   { return nil }

func (c *Cache) refreshListings() {
	if c.listingRefreshMu.TryLock() {
		defer c.listingRefreshMu.Unlock()
	} else {
		return
	}
	// Copy the current torrents to avoid concurrent issues
	c.torrentsMutex.RLock()
	torrents := make([]string, 0, len(c.torrentsNames))
	for k, _ := range c.torrentsNames {
		torrents = append(torrents, k)
	}
	c.torrentsMutex.RUnlock()

	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i] < torrents[j]
	})

	files := make([]os.FileInfo, 0, len(torrents))
	now := time.Now()
	for _, t := range torrents {
		files = append(files, &fileInfo{
			name:    t,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: now,
			isDir:   true,
		})
	}
	// Atomic store of the complete ready-to-use slice
	c.listings.Store(files)
	_ = c.RefreshXml()
	if err := c.RefreshRclone(); err != nil {
		c.logger.Debug().Err(err).Msg("Failed to refresh rclone")
	}
}

func (c *Cache) refreshTorrents() {
	if c.torrentsRefreshMu.TryLock() {
		defer c.torrentsRefreshMu.Unlock()
	} else {
		return
	}
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
	_newTorrents := make([]*types.Torrent, 0)
	idStore := make(map[string]bool, len(debTorrents))
	for _, t := range debTorrents {
		idStore[t.Id] = true
		if _, ok := torrents[t.Id]; !ok {
			_newTorrents = append(_newTorrents, t)
		}
	}

	// Check for deleted torrents
	deletedTorrents := make([]string, 0)
	for id, _ := range torrents {
		if _, ok := idStore[id]; !ok {
			deletedTorrents = append(deletedTorrents, id)
		}
	}
	newTorrents := make([]*types.Torrent, 0)
	for _, t := range _newTorrents {
		if !slices.Contains(deletedTorrents, t.Id) {
			_newTorrents = append(_newTorrents, t)
		}
	}

	if len(deletedTorrents) > 0 {
		c.DeleteTorrents(deletedTorrents)
	}

	if len(newTorrents) == 0 {
		return
	}
	c.logger.Info().Msgf("Found %d new torrents", len(newTorrents))

	// No need for a complex sync process, just add the new torrents
	wg := sync.WaitGroup{}
	wg.Add(len(newTorrents))
	for _, t := range newTorrents {
		// ProcessTorrent is concurrent safe
		go func() {
			defer wg.Done()
			if err := c.ProcessTorrent(t, true); err != nil {
				c.logger.Info().Err(err).Msg("Failed to process torrent")
			}

		}()
	}
	wg.Wait()
}

func (c *Cache) RefreshRclone() error {
	client := request.Default()
	cfg := config.GetConfig().WebDav

	if cfg.RcUrl == "" {
		return nil
	}
	// Create form data
	data := "dir=__all__&dir2=torrents"

	// Create a POST request with form URL-encoded content
	forgetReq, err := http.NewRequest("POST", fmt.Sprintf("%s/vfs/forget", cfg.RcUrl), strings.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.RcUser != "" && cfg.RcPass != "" {
		forgetReq.SetBasicAuth(cfg.RcUser, cfg.RcPass)
	}

	// Set the appropriate content type for form data
	forgetReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send the request
	forgetResp, err := client.Do(forgetReq)
	if err != nil {
		return err
	}
	defer forgetResp.Body.Close()

	if forgetResp.StatusCode != 200 {
		body, _ := io.ReadAll(forgetResp.Body)
		return fmt.Errorf("failed to forget rclone: %s - %s", forgetResp.Status, string(body))
	}
	return nil
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

func (c *Cache) refreshDownloadLinks() {
	if c.downloadLinksRefreshMu.TryLock() {
		defer c.downloadLinksRefreshMu.Unlock()
	} else {
		return
	}
	c.downloadLinksMutex.Lock()
	defer c.downloadLinksMutex.Unlock()

	downloadLinks, err := c.client.GetDownloads()
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to get download links")
	}
	for k, v := range downloadLinks {
		c.downloadLinks[k] = v.DownloadLink
	}
}
