package debrid

import (
	"fmt"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
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
	// COpy the torrents to a string|time map
	torrentsTime := make(map[string]time.Time, c.torrents.Size())
	torrents := make([]string, 0, c.torrents.Size())
	c.torrentsNames.Range(func(key string, value *CachedTorrent) bool {
		torrentsTime[key] = value.AddedOn
		torrents = append(torrents, key)
		return true
	})

	// Sort the torrents by name
	sort.Strings(torrents)

	files := make([]os.FileInfo, 0, len(torrents))
	for _, t := range torrents {
		files = append(files, &fileInfo{
			name:    t,
			size:    0,
			mode:    0755 | os.ModeDir,
			modTime: torrentsTime[t],
			isDir:   true,
		})
	}
	// Atomic store of the complete ready-to-use slice
	c.listings.Store(files)
	_ = c.refreshXml()
	if err := c.RefreshRclone(); err != nil {
		c.logger.Trace().Err(err).Msg("Failed to refresh rclone") // silent error
	}
}

func (c *Cache) refreshTorrents() {
	if c.torrentsRefreshMu.TryLock() {
		defer c.torrentsRefreshMu.Unlock()
	} else {
		return
	}
	// Create a copy of the current torrents to avoid concurrent issues
	torrents := make(map[string]string, c.torrents.Size()) // a mpa of id and name
	c.torrents.Range(func(key string, t *CachedTorrent) bool {
		torrents[t.Id] = t.Name
		return true
	})

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
	idStore := make(map[string]struct{}, len(debTorrents))
	for _, t := range debTorrents {
		idStore[t.Id] = struct{}{}
		if _, ok := torrents[t.Id]; !ok {
			_newTorrents = append(_newTorrents, t)
		}
	}

	// Check for deleted torrents
	deletedTorrents := make([]string, 0)
	for id := range torrents {
		if _, ok := idStore[id]; !ok {
			deletedTorrents = append(deletedTorrents, id)
		}
	}
	newTorrents := make([]*types.Torrent, 0)
	for _, t := range _newTorrents {
		if !slices.Contains(deletedTorrents, t.Id) {
			newTorrents = append(newTorrents, t)
		}
	}

	if len(deletedTorrents) > 0 {
		c.DeleteTorrents(deletedTorrents)
	}

	if len(newTorrents) == 0 {
		return
	}
	c.logger.Info().Msgf("Found %d new torrents", len(newTorrents))

	workChan := make(chan *types.Torrent, min(100, len(newTorrents)))
	errChan := make(chan error, len(newTorrents))
	var wg sync.WaitGroup

	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range workChan {
				select {
				case <-c.ctx.Done():
					return
				default:
				}
				if err := c.ProcessTorrent(t, true); err != nil {
					c.logger.Debug().Err(err).Msgf("Failed to process new torrent %s", t.Id)
					errChan <- err
				}
			}
		}()
	}

	for _, t := range newTorrents {
		select {
		case <-c.ctx.Done():
			break
		default:
			workChan <- t
		}
	}
	close(workChan)
	wg.Wait()

	c.logger.Debug().Msgf("Processed %d new torrents", len(newTorrents))
}

func (c *Cache) RefreshRclone() error {
	client := request.Default()
	cfg := config.Get().WebDav

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
	addedOn, err := time.Parse(time.RFC3339, _torrent.Added)
	if err != nil {
		addedOn = time.Now()
	}
	ct := &CachedTorrent{
		Torrent:    _torrent,
		AddedOn:    addedOn,
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

	downloadLinks, err := c.client.GetDownloads()
	if err != nil {
		c.logger.Debug().Err(err).Msg("Failed to get download links")
	}
	for k, v := range downloadLinks {
		// if link is generated in the last 24 hours, add it to cache
		timeSince := time.Since(v.Generated)
		if timeSince < c.autoExpiresLinksAfter {
			c.downloadLinks.Store(k, downloadLinkCache{
				Link:      v.DownloadLink,
				ExpiresAt: v.Generated.Add(c.autoExpiresLinksAfter - timeSince),
			})
		} else {
			c.downloadLinks.Delete(k)
		}
	}

	c.logger.Debug().Msgf("Refreshed %d download links", len(downloadLinks))

}
