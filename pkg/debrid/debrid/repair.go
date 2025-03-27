package debrid

import (
	"errors"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
	"slices"
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
				if errors.Is(err, request.ErrLinkBroken) {
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
	c.logger.Info().Msg("Starting repair worker")

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

			// Check if torrent is broken
			if c.IsTorrentBroken(cachedTorrent, nil) {
				c.logger.Info().Str("torrentId", torrentId).Msg("Repairing broken torrent")
				// Repair torrent
				if err := c.repairTorrent(cachedTorrent); err != nil {
					c.logger.Error().Err(err).Str("torrentId", torrentId).Msg("Failed to repair torrent")
				} else {
					c.logger.Info().Str("torrentId", torrentId).Msg("Torrent repaired")
				}
			} else {
				c.logger.Debug().Str("torrentId", torrentId).Msg("Torrent is not broken")
			}
			c.repairsInProgress.Delete(torrentId)
		}
	}
}

func (c *Cache) SubmitForRepair(torrentId, fileName string) {
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

func (c *Cache) repairTorrent(t *CachedTorrent) error {
	// Check if Magnet is not empty, if empty, reconstruct the magnet

	if _, inProgress := c.repairsInProgress.Load(t.Id); inProgress {
		c.logger.Debug().Str("torrentID", t.Id).Msg("Repair already in progress")
		return nil
	}

	torrent := t.Torrent
	if torrent.Magnet == nil {
		torrent.Magnet = utils.ConstructMagnet(t.InfoHash, t.Name)
	}

	oldID := torrent.Id

	// Submit the magnet to the debrid service
	torrent.Id = ""
	var err error
	torrent, err = c.client.SubmitMagnet(torrent)
	if err != nil {
		return fmt.Errorf("failed to submit magnet: %w", err)
	}

	// Check if the torrent was submitted
	if torrent == nil || torrent.Id == "" {
		return fmt.Errorf("failed to submit magnet: empty torrent")
	}
	torrent, err = c.client.CheckStatus(torrent, true)
	if err != nil {
		return fmt.Errorf("failed to check status: %w", err)
	}

	c.client.DeleteTorrent(oldID) // delete the old torrent
	c.DeleteTorrent(oldID)        // Remove from listings

	// Update the torrent in the cache
	t.Torrent = torrent
	c.setTorrent(t)
	c.refreshListings()

	c.repairsInProgress.Delete(oldID)
	return nil
}
