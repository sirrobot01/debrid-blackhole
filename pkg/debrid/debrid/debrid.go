package debrid

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/alldebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid_link"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/realdebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torbox"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/types"
)

func createDebridClient(dc config.Debrid) types.Client {
	switch dc.Name {
	case "realdebrid":
		return realdebrid.New(dc)
	case "torbox":
		return torbox.New(dc)
	case "debridlink":
		return debrid_link.New(dc)
	case "alldebrid":
		return alldebrid.New(dc)
	default:
		return realdebrid.New(dc)
	}
}

func ProcessTorrent(d *Engine, magnet *utils.Magnet, a *arr.Arr, isSymlink, overrideDownloadUncached bool) (*types.Torrent, error) {

	debridTorrent := &types.Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      a,
		Size:     magnet.Size,
		Files:    make(map[string]types.File),
	}

	errs := make([]error, 0)

	for index, db := range d.Clients {
		logger := db.GetLogger()
		logger.Info().Msgf("Processing debrid: %s", db.GetName())

		// Override first, arr second, debrid third

		if overrideDownloadUncached {
			debridTorrent.DownloadUncached = true
		} else if a.DownloadUncached != nil {
			// Arr cached is set
			debridTorrent.DownloadUncached = *a.DownloadUncached
		} else {
			debridTorrent.DownloadUncached = db.GetDownloadUncached()
		}

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
		logger.Info().Msgf("Torrent: %s(id=%s) submitted to %s", dbt.Name, dbt.Id, db.GetName())
		d.LastUsed = index
		return db.CheckStatus(dbt, isSymlink)
	}
	err := fmt.Errorf("failed to process torrent")
	for _, e := range errs {
		err = fmt.Errorf("%w\n%w", err, e)
	}
	return nil, err
}
