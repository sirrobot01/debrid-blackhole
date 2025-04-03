package debrid

import (
	"errors"
	"fmt"
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
				} else {
					// This might just be a temporary error
				}
			} else {
				// Generate a new download link?
			}
		}
	}
	return isBroken
}

func (c *Cache) repairWorker() {
	// This watches a channel for torrents to repair
	for {
		select {
		case req := <-c.repairChan:
			torrentId := req.TorrentID
			if _, inProgress := c.repairsInProgress.Load(torrentId); inProgress {
				c.logger.Debug().Str("torrentId", torrentId).Msg("Skipping duplicate repair request")
				continue
			}

			// Mark as in progress
			c.repairsInProgress.Store(torrentId, true)
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
				c.reInsertTorrent(cachedTorrent)
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
}

func (c *Cache) reInsertTorrent(t *CachedTorrent) {
	// Reinsert the torrent into the cache
	c.torrents.Store(t.Id, t)
	c.logger.Debug().Str("torrentId", t.Id).Msg("Reinserted torrent into cache")
}

func (c *Cache) submitForRepair(repairType RepairType, torrentId, fileName string) {
	// Submitting a torrent for repair.Not used yet

	// Check if already in progress before even submitting
	if _, inProgress := c.repairsInProgress.Load(torrentId); inProgress {
		c.logger.Debug().Str("torrentID", torrentId).Msg("Repair already in progress")
		return
	}

	select {
	case c.repairChan <- RepairRequest{TorrentID: torrentId, FileName: fileName}:
		c.logger.Debug().Str("torrentID", torrentId).Msg("Submitted for repair")
	default:
		c.logger.Warn().Str("torrentID", torrentId).Msg("Repair channel full, skipping repair request")
	}
}

func (c *Cache) ReInsertTorrent(torrent *types.Torrent) error {
	// Check if Magnet is not empty, if empty, reconstruct the magnet
	if _, ok := c.repairsInProgress.Load(torrent.Id); ok {
		return fmt.Errorf("repair already in progress for torrent %s", torrent.Id)
	}

	if torrent.Magnet == nil {
		torrent.Magnet = utils.ConstructMagnet(torrent.InfoHash, torrent.Name)
	}

	oldID := torrent.Id
	defer c.repairsInProgress.Delete(oldID)
	defer c.DeleteTorrent(oldID)

	// Submit the magnet to the debrid service
	torrent.Id = ""
	var err error
	torrent, err = c.client.SubmitMagnet(torrent)
	if err != nil {
		// Remove the old torrent from the cache and debrid service
		return fmt.Errorf("failed to submit magnet: %w", err)
	}

	// Check if the torrent was submitted
	if torrent == nil || torrent.Id == "" {
		return fmt.Errorf("failed to submit magnet: empty torrent")
	}
	torrent.DownloadUncached = false // Set to false, avoid re-downloading
	torrent, err = c.client.CheckStatus(torrent, true)
	if err != nil && torrent != nil {
		// Torrent is likely in progress
		_ = c.DeleteTorrent(torrent.Id)

		return fmt.Errorf("failed to check status: %w", err)
	}

	if torrent == nil {
		return fmt.Errorf("failed to check status: empty torrent")
	}

	for _, file := range torrent.Files {
		if file.Link == "" {
			c.logger.Debug().Msgf("Torrent %s is still not complete, missing link for file %s.", torrent.Name, file.Name)
			// Delete the torrent from the cache
			_ = c.DeleteTorrent(torrent.Id)
			return fmt.Errorf("torrent %s is still not complete, missing link for file %s", torrent.Name, file.Name)
		}
	}

	// Update the torrent in the cache
	addedOn, err := time.Parse(time.RFC3339, torrent.Added)
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
	return nil
}
