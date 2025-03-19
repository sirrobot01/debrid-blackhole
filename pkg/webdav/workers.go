package webdav

import "time"

func (c *Cache) Refresh() error {
	// For now, we just want to refresh the listing and download links
	c.logger.Info().Msg("Starting cache refresh workers")
	go c.refreshListingWorker()
	go c.refreshDownloadLinksWorker()
	go c.refreshTorrentsWorker()
	return nil
}

func (c *Cache) refreshListingWorker() {
	refreshTicker := time.NewTicker(10 * time.Second)
	defer refreshTicker.Stop()

	for {
		select {
		case <-refreshTicker.C:
			if c.listingRefreshMu.TryLock() {
				func() {
					defer c.listingRefreshMu.Unlock()
					c.refreshListings()
				}()
			} else {
				c.logger.Debug().Msg("Refresh already in progress")
			}
		}
	}
}

func (c *Cache) refreshDownloadLinksWorker() {
	refreshTicker := time.NewTicker(40 * time.Minute)
	defer refreshTicker.Stop()

	for {
		select {
		case <-refreshTicker.C:
			if c.downloadLinksRefreshMu.TryLock() {
				func() {
					defer c.downloadLinksRefreshMu.Unlock()
					c.refreshDownloadLinks()
				}()
			} else {
				c.logger.Debug().Msg("Refresh already in progress")
			}
		}
	}
}

func (c *Cache) refreshTorrentsWorker() {
	refreshTicker := time.NewTicker(5 * time.Second)
	defer refreshTicker.Stop()

	for {
		select {
		case <-refreshTicker.C:
			if c.listingRefreshMu.TryLock() {
				func() {
					defer c.listingRefreshMu.Unlock()
					c.refreshTorrents()
				}()
			} else {
				c.logger.Debug().Msg("Refresh already in progress")
			}
		}
	}
}
