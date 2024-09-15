package debrid

import (
	"fmt"
	"github.com/anacrolix/torrent/metainfo"
	"goBlack/common"
	"log"
	"path/filepath"
)

type Service interface {
	SubmitMagnet(torrent *Torrent) (*Torrent, error)
	CheckStatus(torrent *Torrent) (*Torrent, error)
	DownloadLink(torrent *Torrent) error
	IsAvailable(infohashes []string) map[string]bool
	GetMountPath() string
	GetDownloadUncached() bool
	GetTorrent(id string) (*Torrent, error)
	GetName() string
	GetLogger() *log.Logger
}

type Debrid struct {
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
	client           *common.RLHTTPClient
	cache            *common.Cache
	MountPath        string
	logger           *log.Logger
}

func NewDebrid(dc common.DebridConfig, cache *common.Cache) Service {
	switch dc.Name {
	case "realdebrid":
		return NewRealDebrid(dc, cache)
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
	hashes := make([]string, len(infohashes))

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

	return hashes, result
}

func ProcessQBitTorrent(d Service, magnet *common.Magnet, arr *Arr) (*Torrent, error) {
	debridTorrent := &Torrent{
		InfoHash: magnet.InfoHash,
		Magnet:   magnet,
		Name:     magnet.Name,
		Arr:      arr,
		Size:     magnet.Size,
	}
	logger := d.GetLogger()
	logger.Printf("Torrent Hash: %s", debridTorrent.InfoHash)
	if !d.GetDownloadUncached() {
		hash, exists := d.IsAvailable([]string{debridTorrent.InfoHash})[debridTorrent.InfoHash]
		if !exists || !hash {
			return debridTorrent, fmt.Errorf("torrent: %s is not cached", debridTorrent.Name)
		} else {
			logger.Printf("Torrent: %s is cached", debridTorrent.Name)
		}
	}

	debridTorrent, err := d.SubmitMagnet(debridTorrent)
	if err != nil || debridTorrent.Id == "" {
		logger.Printf("Error submitting magnet: %s", err)
		return nil, err
	}
	return d.CheckStatus(debridTorrent)
}
