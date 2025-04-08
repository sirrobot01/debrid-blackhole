package debrid

import "time"

func (c *Cache) Refresh() error {
	// For now, we just want to refresh the listing and download links
	go c.refreshDownloadLinksWorker()
	go c.refreshTorrentsWorker()
	return nil
}

func (c *Cache) refreshDownloadLinksWorker() {
	refreshTicker := time.NewTicker(c.downloadLinksRefreshInterval)
	defer refreshTicker.Stop()

	for range refreshTicker.C {
		c.refreshDownloadLinks()
	}
}

func (c *Cache) refreshTorrentsWorker() {
	refreshTicker := time.NewTicker(c.torrentRefreshInterval)
	defer refreshTicker.Stop()

	for range refreshTicker.C {
		c.refreshTorrents()
	}
}
