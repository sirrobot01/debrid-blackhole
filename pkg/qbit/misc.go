package qbit

import (
	"github.com/google/uuid"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	"strings"
)

func CreateTorrentFromMagnet(magnet *utils.Magnet, category, source string) *Torrent {
	torrent := &Torrent{
		ID:        uuid.NewString(),
		Hash:      strings.ToLower(magnet.InfoHash),
		Name:      magnet.Name,
		Size:      magnet.Size,
		Category:  category,
		Source:    source,
		State:     "downloading",
		MagnetUri: magnet.Link,

		Tracker:    "udp://tracker.opentrackr.org:1337",
		UpLimit:    -1,
		DlLimit:    -1,
		AutoTmm:    false,
		Ratio:      1,
		RatioLimit: 1,
	}
	return torrent
}
