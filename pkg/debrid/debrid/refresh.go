package debrid

import (
	"bytes"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

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
	newTorrents := make([]*types.Torrent, 0)
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
	params := map[string]interface{}{
		"recursive": "false",
	}

	// Convert parameters to JSON
	jsonParams, err := json.Marshal(params)
	if err != nil {
		return err
	}

	// Create HTTP request
	url := "http://192.168.0.219:9990/vfs/refresh" // Switch to config
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonParams))
	if err != nil {
		return err
	}

	// Set the appropriate headers
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to refresh rclone: %s", resp.Status)
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
