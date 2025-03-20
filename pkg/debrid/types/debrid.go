package types

import (
	"github.com/rs/zerolog"
)

type Client interface {
	SubmitMagnet(tr *Torrent) (*Torrent, error)
	CheckStatus(tr *Torrent, isSymlink bool) (*Torrent, error)
	GenerateDownloadLinks(tr *Torrent) error
	GetDownloadLink(tr *Torrent, file *File) *File
	ConvertLinksToFiles(links []string) []File
	DeleteTorrent(tr *Torrent)
	IsAvailable(infohashes []string) map[string]bool
	GetCheckCached() bool
	GetDownloadUncached() bool
	UpdateTorrent(torrent *Torrent) error
	GetTorrents() ([]*Torrent, error)
	GetName() string
	GetLogger() zerolog.Logger
	GetDownloadingStatus() []string
	GetDownloads() (map[string]DownloadLinks, error)
}
