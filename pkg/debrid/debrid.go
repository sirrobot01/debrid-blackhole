package debrid

import (
	"fmt"
	"github.com/anacrolix/torrent/metainfo"
	"goBlack/common"
	"log"
	"path/filepath"
)

type BaseDebrid struct {
	Name             string
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *common.RLHTTPClient
	cache            *common.Cache
	MountPath        string
	logger           *log.Logger
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
	GetLogger() *log.Logger
}

func NewDebrid(debs []common.DebridConfig, cache *common.Cache) *DebridService {
	debrids := make([]Service, 0)
	for _, dc := range debs {
		d := createDebrid(dc, cache)
		d.GetLogger().Println("Debrid Service started")
		debrids = append(debrids, d)
	}
	d := &DebridService{debrids: debrids, lastUsed: 0}
	return d
}

func createDebrid(dc common.DebridConfig, cache *common.Cache) Service {
	switch dc.Name {
	case "realdebrid":
		return NewRealDebrid(dc, cache)
	case "torbox":
		return NewTorbox(dc, cache)
	case "debridlink":
		return NewDebridLink(dc, cache)
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
	magnetLink := common.OpenMagnetFile(filePath)
	magnet, err := common.GetMagnetInfo(magnetLink)
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
	magnet := &common.Magnet{
		InfoHash: infoHash,
		Name:     info.Name,
		Size:     info.Length,
		Link:     mi.Magnet(&hash, &info).String(),
	}
	torrent := &Torrent{
		InfoHash: infoHash,
		Name:     info.Name,
		Size:     info.Length,
		Magnet:   magnet,
		Filename: filePath,
	}
	return torrent, nil
}

func GetLocalCache(infohashes []string, cache *common.Cache) ([]string, map[string]bool) {
	result := make(map[string]bool)

	//if len(infohashes) == 0 {
	//	return hashes, result
	//}
	//if len(infohashes) == 1 {
	//	if cache.Exists(infohashes[0]) {
	//		return hashes, map[string]bool{infohashes[0]: true}
	//	}
	//	return infohashes, result
	//}
	//
	//cachedHashes := cache.GetMultiple(infohashes)
	//for _, h := range infohashes {
	//	_, exists := cachedHashes[h]
	//	if !exists {
	//		hashes = append(hashes, h)
	//	} else {
	//		result[h] = true
	//	}
	//}

	return infohashes, result
}

func ProcessQBitTorrent(d *DebridService, magnet *common.Magnet, arr *Arr, isSymlink bool) (*Torrent, error) {
	debridTorrent := &Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      arr,
		Size:     magnet.Size,
	}

	for index, db := range d.debrids {
		log.Println("Processing debrid: ", db.GetName())
		logger := db.GetLogger()
		logger.Printf("Torrent Hash: %s", debridTorrent.InfoHash)
		if !db.GetCheckCached() {
			hash, exists := db.IsAvailable([]string{debridTorrent.InfoHash})[debridTorrent.InfoHash]
			if !exists || !hash {
				logger.Printf("Torrent: %s is not cached", debridTorrent.Name)
				continue
			} else {
				logger.Printf("Torrent: %s is cached(or downloading)", debridTorrent.Name)
			}
		}

		debridTorrent, err := db.SubmitMagnet(debridTorrent)
		if err != nil || debridTorrent.Id == "" {
			logger.Printf("Error submitting magnet: %s", err)
			continue
		}
		logger.Printf("Torrent: %s submitted to %s", debridTorrent.Name, db.GetName())
		d.lastUsed = index
		debridTorrent.Debrid = db
		return db.CheckStatus(debridTorrent, isSymlink)
	}
	return nil, fmt.Errorf("failed to process torrent")
}
