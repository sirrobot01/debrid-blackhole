package debrid

import "time"

func (c *Cache) Refresh() error {
	// For now, we just want to refresh the listing and download links
	go c.refreshDownloadLinksWorker()
	go c.refreshTorrentsWorker()
	go c.resetInvalidLinksWorker()
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

func (c *Cache) resetInvalidLinksWorker() {
	// Calculate time until next 00:00 CET
	now := time.Now()
	loc, err := time.LoadLocation("CET")
	if err != nil {
		// Fallback if CET timezone can't be loaded
		c.logger.Error().Err(err).Msg("Failed to load CET timezone, using local time")
		loc = time.Local
	}

	nowInCET := now.In(loc)
	next := time.Date(
		nowInCET.Year(),
		nowInCET.Month(),
		nowInCET.Day(),
		0, 0, 0, 0,
		loc,
	)

	// If it's already past 12:00 CET today, schedule for tomorrow
	if nowInCET.After(next) {
		next = next.Add(24 * time.Hour)
	}

	// Duration until next 12:00 CET
	initialWait := next.Sub(nowInCET)

	// Set up initial timer
	timer := time.NewTimer(initialWait)
	defer timer.Stop()

	c.logger.Debug().Msgf("Scheduled invalid links reset at %s (in %s)", next.Format("2006-01-02 15:04:05 MST"), initialWait)

	// Wait for the first execution
	<-timer.C
	c.resetInvalidLinks()

	// Now set up the daily ticker
	refreshTicker := time.NewTicker(24 * time.Hour)
	defer refreshTicker.Stop()

	for range refreshTicker.C {
		c.resetInvalidLinks()
	}
}
