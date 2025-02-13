package debrid

import (
	"cmp"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/alldebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid_link"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/realdebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torbox"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

func New() *engine.Engine {
	cfg := config.GetConfig()
	maxCachedSize := cmp.Or(cfg.MaxCacheSize, 1000)
	debrids := make([]engine.Service, 0)
	// Divide the cache size by the number of debrids
	maxCacheSize := maxCachedSize / len(cfg.Debrids)

	for _, dc := range cfg.Debrids {
		d := createDebrid(dc, common.NewCache(maxCacheSize))
		logger := d.GetLogger()
		logger.Info().Msg("Debrid Service started")
		debrids = append(debrids, d)
	}
	d := &engine.Engine{Debrids: debrids, LastUsed: 0}
	return d
}

func createDebrid(dc config.Debrid, cache *common.Cache) engine.Service {
	switch dc.Name {
	case "realdebrid":
		return realdebrid.New(dc, cache)
	case "torbox":
		return torbox.New(dc, cache)
	case "debridlink":
		return debrid_link.New(dc, cache)
	case "alldebrid":
		return alldebrid.New(dc, cache)
	default:
		return realdebrid.New(dc, cache)
	}
}

func ProcessTorrent(d *engine.Engine, magnet *utils.Magnet, a *arr.Arr, isSymlink bool) (*torrent.Torrent, error) {
	debridTorrent := &torrent.Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      a,
		Size:     magnet.Size,
	}

	errs := make([]error, 0)

	for index, db := range d.Debrids {
		logger := db.GetLogger()
		logger.Info().Msgf("Processing debrid: %s", db.GetName())

		logger.Info().Msgf("Torrent Hash: %s", debridTorrent.InfoHash)
		if db.GetCheckCached() {
			hash, exists := db.IsAvailable([]string{debridTorrent.InfoHash})[debridTorrent.InfoHash]
			if !exists || !hash {
				logger.Info().Msgf("Torrent: %s is not cached", debridTorrent.Name)
				continue
			} else {
				logger.Info().Msgf("Torrent: %s is cached(or downloading)", debridTorrent.Name)
			}
		}

		dbt, err := db.SubmitMagnet(debridTorrent)
		if dbt != nil {
			dbt.Arr = a
		}
		if err != nil || dbt == nil || dbt.Id == "" {
			errs = append(errs, err)
			continue
		}
		logger.Info().Msgf("Torrent: %s submitted to %s", dbt.Name, db.GetName())
		d.LastUsed = index
		return db.CheckStatus(dbt, isSymlink)
	}
	err := fmt.Errorf("failed to process torrent")
	for _, e := range errs {
		err = fmt.Errorf("%w\n%w", err, e)
	}
	return nil, err
}
