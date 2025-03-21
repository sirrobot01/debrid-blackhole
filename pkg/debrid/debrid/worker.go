package debrid

import "time"

func (c *Cache) Refresh() error {
	// For now, we just want to refresh the listing and download links
	c.logger.Info().Msg("Starting cache refresh workers")
	go c.refreshDownloadLinksWorker()
	go c.refreshTorrentsWorker()
	return nil
}

func (c *Cache) refreshDownloadLinksWorker() {
	refreshTicker := time.NewTicker(c.downloadLinksRefreshInterval)
	defer refreshTicker.Stop()

	for {
		select {
		case <-refreshTicker.C:
			c.refreshDownloadLinks()
		}
	}
}

func (c *Cache) refreshTorrentsWorker() {
	refreshTicker := time.NewTicker(c.torrentRefreshInterval)
	defer refreshTicker.Stop()

	for {
		select {
		case <-refreshTicker.C:
			c.refreshTorrents()
		}
	}
}
