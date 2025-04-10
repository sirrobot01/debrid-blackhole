package debrid

import (
	"errors"
	"fmt"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"slices"
	"time"
)

func (c *Cache) IsTorrentBroken(t *CachedTorrent, filenames []string) bool {
	// Check torrent files

	isBroken := false
	files := make(map[string]types.File)
	if len(filenames) > 0 {
		for name, f := range t.Files {
			if slices.Contains(filenames, name) {
				files[name] = f
			}
		}
	} else {
		files = t.Files
	}

	// Check empty links
	for _, f := range files {
		// Check if file is missing
		if f.Link == "" {
			// refresh torrent and then break
			t = c.refreshTorrent(t)
			break
		}
	}

	files = t.Files

	for _, f := range files {
		// Check if file link is still missing
		if f.Link == "" {
			isBroken = true
			break
		} else {
			// Check if file.Link not in the downloadLink Cache
			if err := c.client.CheckLink(f.Link); err != nil {
				if errors.Is(err, request.HosterUnavailableError) {
					isBroken = true
					break
				}
			}
		}
	}
	return isBroken
}

func (c *Cache) repairWorker() {
	// This watches a channel for torrents to repair
	for req := range c.repairChan {
		torrentId := req.TorrentID
		if _, inProgress := c.repairsInProgress.Load(torrentId); inProgress {
			c.logger.Debug().Str("torrentId", torrentId).Msg("Skipping duplicate repair request")
			continue
		}

		// Mark as in progress
		c.repairsInProgress.Store(torrentId, struct{}{})
		c.logger.Debug().Str("torrentId", req.TorrentID).Msg("Received repair request")

		// Get the torrent from the cache
		cachedTorrent, ok := c.torrents.Load(torrentId)
		if !ok || cachedTorrent == nil {
			c.logger.Warn().Str("torrentId", torrentId).Msg("Torrent not found in cache")
			continue
		}

		switch req.Type {
		case RepairTypeReinsert:
			c.logger.Debug().Str("torrentId", torrentId).Msg("Reinserting torrent")
			var err error
			cachedTorrent, err = c.reInsertTorrent(cachedTorrent.Torrent)
			if err != nil {
				c.logger.Error().Err(err).Str("torrentId", cachedTorrent.Id).Msg("Failed to reinsert torrent")
				continue
			}
		case RepairTypeDelete:
			c.logger.Debug().Str("torrentId", torrentId).Msg("Deleting torrent")
			if err := c.DeleteTorrent(torrentId); err != nil {
				c.logger.Error().Err(err).Str("torrentId", torrentId).Msg("Failed to delete torrent")
				continue
			}
		}
		c.repairsInProgress.Delete(torrentId)
	}
}

func (c *Cache) reInsertTorrent(torrent *types.Torrent) (*CachedTorrent, error) {
	// Check if Magnet is not empty, if empty, reconstruct the magnet
	if _, ok := c.repairsInProgress.Load(torrent.Id); ok {
		return nil, fmt.Errorf("repair already in progress for torrent %s", torrent.Id)
	}

	if torrent.Magnet == nil {
		torrent.Magnet = utils.ConstructMagnet(torrent.InfoHash, torrent.Name)
	}

	oldID := torrent.Id
	defer func() {
		err := c.DeleteTorrent(oldID)
		if err != nil {
			c.logger.Error().Err(err).Str("torrentId", oldID).Msg("Failed to delete old torrent")
		}
	}()

	// Submit the magnet to the debrid service
	torrent.Id = ""
	var err error
	torrent, err = c.client.SubmitMagnet(torrent)
	if err != nil {
		// Remove the old torrent from the cache and debrid service
		return nil, fmt.Errorf("failed to submit magnet: %w", err)
	}

	// Check if the torrent was submitted
	if torrent == nil || torrent.Id == "" {
		return nil, fmt.Errorf("failed to submit magnet: empty torrent")
	}
	torrent.DownloadUncached = false // Set to false, avoid re-downloading
	torrent, err = c.client.CheckStatus(torrent, true)
	if err != nil && torrent != nil {
		// Torrent is likely in progress
		_ = c.DeleteTorrent(torrent.Id)

		return nil, fmt.Errorf("failed to check status: %w", err)
	}

	if torrent == nil {
		return nil, fmt.Errorf("failed to check status: empty torrent")
	}

	// Update the torrent in the cache
	addedOn, err := time.Parse(time.RFC3339, torrent.Added)
	for _, f := range torrent.Files {
		if f.Link == "" {
			// Delete the new torrent
			_ = c.DeleteTorrent(torrent.Id)
			return nil, fmt.Errorf("failed to reinsert torrent: empty link")
		}
	}
	if err != nil {
		addedOn = time.Now()
	}
	ct := &CachedTorrent{
		Torrent:    torrent,
		IsComplete: len(torrent.Files) > 0,
		AddedOn:    addedOn,
	}
	c.setTorrent(ct)
	c.refreshListings()
	return ct, nil
}

func (c *Cache) resetInvalidLinks() {
	c.invalidDownloadLinks = xsync.NewMapOf[string, string]()
	c.client.ResetActiveDownloadKeys() // Reset the active download keys
}
