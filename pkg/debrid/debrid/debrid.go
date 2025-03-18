package debrid

import (
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
)

type Client interface {
	SubmitMagnet(tr *torrent.Torrent) (*torrent.Torrent, error)
	CheckStatus(tr *torrent.Torrent, isSymlink bool) (*torrent.Torrent, error)
	GenerateDownloadLinks(tr *torrent.Torrent) error
	GetDownloadLink(tr *torrent.Torrent, file *torrent.File) *torrent.File
	ConvertLinksToFiles(links []string) []torrent.File
	DeleteTorrent(tr *torrent.Torrent)
	IsAvailable(infohashes []string) map[string]bool
	GetCheckCached() bool
	GetDownloadUncached() bool
	UpdateTorrent(torrent *torrent.Torrent) error
	GetTorrents() ([]*torrent.Torrent, error)
	GetName() string
	GetLogger() zerolog.Logger
	GetDownloadingStatus() []string
	GetDownloads() (map[string]torrent.DownloadLinks, error)
}
