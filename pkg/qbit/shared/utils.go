package shared

import (
	"github.com/sirrobot01/debrid-blackhole/common"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"path/filepath"
	"sync"
	"time"
)

func checkFileLoop(wg *sync.WaitGroup, dir string, file debrid.TorrentFile, ready chan<- debrid.TorrentFile) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()
	path := filepath.Join(dir, file.Path)
	for {
		select {
		case <-ticker.C:
			if common.FileReady(path) {
				ready <- file
				return
			}
		}
	}
}
