package qbit

import (
	"github.com/google/uuid"
	"github.com/sirrobot01/debrid-blackhole/internal/utils"
	debrid "github.com/sirrobot01/debrid-blackhole/pkg/debrid/torrent"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func checkFileLoop(wg *sync.WaitGroup, dir string, file debrid.File, ready chan<- debrid.File) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()
	path := filepath.Join(dir, file.Path)
	for {
		select {
		case <-ticker.C:
			_, err := os.Stat(path)
			if !os.IsNotExist(err) {
				ready <- file
				return
			}
		}
	}
}

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
