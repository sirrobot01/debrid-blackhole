package debrid

import (
	"cmp"
	"fmt"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"path/filepath"
)

type BaseDebrid struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *request.RLHTTPClient
	cache            *common.Cache
	MountPath        string
	logger           zerolog.Logger
	CheckCached      bool
}

type Service interface {
	SubmitMagnet(torrent *Torrent) (*Torrent, error)
	CheckStatus(torrent *Torrent, isSymlink bool) (*Torrent, error)
	GetDownloadLinks(torrent *Torrent) error
	DeleteTorrent(torrent *Torrent)
	IsAvailable(infohashes []string) map[string]bool
	GetMountPath() string
	GetCheckCached() bool
	GetTorrent(id string) (*Torrent, error)
	GetName() string
	GetLogger() zerolog.Logger
}

func NewDebrid() *DebridService {
	cfg := config.GetConfig()
	maxCachedSize := cmp.Or(cfg.MaxCacheSize, 1000)
	debrids := make([]Service, 0)
	// Divide the cache size by the number of debrids
	maxCacheSize := maxCachedSize / len(cfg.Debrids)

	for _, dc := range cfg.Debrids {
		d := createDebrid(dc, common.NewCache(maxCacheSize))
		logger := d.GetLogger()
		logger.Info().Msg("Debrid Service started")
		debrids = append(debrids, d)
	}
	d := &DebridService{debrids: debrids, lastUsed: 0}
	return d
}

func createDebrid(dc config.Debrid, cache *common.Cache) Service {
	switch dc.Name {
	case "realdebrid":
		return NewRealDebrid(dc, cache)
	case "torbox":
		return NewTorbox(dc, cache)
	case "debridlink":
		return NewDebridLink(dc, cache)
	case "alldebrid":
		return NewAllDebrid(dc, cache)
	default:
		return NewRealDebrid(dc, cache)
	}
}

func GetTorrentInfo(filePath string) (*Torrent, error) {
	// Open and read the .torrent file
	if filepath.Ext(filePath) == ".torrent" {
		return getTorrentInfo(filePath)
	} else {
		return torrentFromMagnetFile(filePath)
	}

}

func torrentFromMagnetFile(filePath string) (*Torrent, error) {
	magnetLink := utils.OpenMagnetFile(filePath)
	magnet, err := utils.GetMagnetInfo(magnetLink)
	if err != nil {
		return nil, err
	}
	torrent := &Torrent{
		InfoHash: magnet.InfoHash,
		Name:     magnet.Name,
		Size:     magnet.Size,
		Magnet:   magnet,
		Filename: filePath,
	}
	return torrent, nil
}

func getTorrentInfo(filePath string) (*Torrent, error) {
	mi, err := metainfo.LoadFromFile(filePath)
	if err != nil {
		return nil, err
	}
	hash := mi.HashInfoBytes()
	infoHash := hash.HexString()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	infoLength := info.Length
	magnet := &utils.Magnet{
		InfoHash: infoHash,
		Name:     info.Name,
		Size:     infoLength,
		Link:     mi.Magnet(&hash, &info).String(),
	}
	torrent := &Torrent{
		InfoHash: infoHash,
		Name:     info.Name,
		Size:     infoLength,
		Magnet:   magnet,
		Filename: filePath,
	}
	return torrent, nil
}

func GetLocalCache(infohashes []string, cache *common.Cache) ([]string, map[string]bool) {
	result := make(map[string]bool)
	hashes := make([]string, 0)

	if len(infohashes) == 0 {
		return hashes, result
	}
	if len(infohashes) == 1 {
		if cache.Exists(infohashes[0]) {
			return hashes, map[string]bool{infohashes[0]: true}
		}
		return infohashes, result
	}

	cachedHashes := cache.GetMultiple(infohashes)
	for _, h := range infohashes {
		_, exists := cachedHashes[h]
		if !exists {
			hashes = append(hashes, h)
		} else {
			result[h] = true
		}
	}

	return infohashes, result
}

func ProcessTorrent(d *DebridService, magnet *utils.Magnet, a *arr.Arr, isSymlink bool) (*Torrent, error) {
	debridTorrent := &Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      a,
		Size:     magnet.Size,
	}

	errs := make([]error, 0)

	for index, db := range d.debrids {
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
			dbt.Debrid = db
			dbt.Arr = a
		}
		if err != nil || dbt == nil || dbt.Id == "" {
			errs = append(errs, err)
			continue
		}
		logger.Info().Msgf("Torrent: %s submitted to %s", dbt.Name, db.GetName())
		d.lastUsed = index
		return db.CheckStatus(dbt, isSymlink)
	}
	err := fmt.Errorf("failed to process torrent")
	for _, e := range errs {
		err = fmt.Errorf("%w\n%w", err, e)
	}
	return nil, err
}
