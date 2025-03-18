package debrid

import (
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/alldebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid_link"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/realdebrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torbox"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

func New() *engine.Engine {
	cfg := config.GetConfig()
	debrids := make([]debrid.Client, 0)

	for _, dc := range cfg.Debrids {
		client := createDebridClient(dc)
		logger := client.GetLogger()
		logger.Info().Msg("Debrid Service started")
		debrids = append(debrids, client)
	}
	d := &engine.Engine{
		Debrids:  debrids,
		LastUsed: 0,
	}
	return d
}

func createDebridClient(dc config.Debrid) debrid.Client {
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

func ProcessTorrent(d *engine.Engine, magnet *utils.Magnet, a *arr.Arr, isSymlink, overrideDownloadUncached bool) (*torrent.Torrent, error) {

	debridTorrent := &torrent.Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      a,
		Size:     magnet.Size,
		Files:    make(map[string]torrent.File),
	}

	errs := make([]error, 0)

	for index, db := range d.Debrids {
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
