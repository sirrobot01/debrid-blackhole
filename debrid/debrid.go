package debrid

import (
	"github.com/anacrolix/torrent/metainfo"
	"goBlack/common"
	"path/filepath"
)

type Service interface {
	SubmitMagnet(torrent *Torrent) (*Torrent, error)
	CheckStatus(torrent *Torrent) (*Torrent, error)
	DownloadLink(torrent *Torrent) error
	Process(arr *Arr, magnet string) (*Torrent, error)
	IsAvailable(infohashes []string) map[string]bool
}

type Debrid struct {
	Host             string `json:"host"`
	APIKey           string
	DownloadUncached bool
}

func NewDebrid(dc common.DebridConfig) Service {
	switch dc.Name {
	case "realdebrid":
		return NewRealDebrid(dc)
	default:
		return NewRealDebrid(dc)
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
