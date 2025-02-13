package engine

import (
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

type Service interface {
	SubmitMagnet(tr *torrent.Torrent) (*torrent.Torrent, error)
	CheckStatus(tr *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error)
	GetDownloadLinks(tr *torrent.Torrent) error
	GetDownloadLink(tr *torrent.Torrent, file *torrent.File) *torrent.DownloadLinks
	DeleteTorrent(tr *torrent.Torrent)
	IsAvailable(infohashes []string) map[string]bool
	GetMountPath() string
	GetCheckCached() bool
	GetTorrent(id string) (*torrent.Torrent, error)
	GetTorrents() ([]*torrent.Torrent, error)
	GetName() string
	GetLogger() zerolog.Logger
}
