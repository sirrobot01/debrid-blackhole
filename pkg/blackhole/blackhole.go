package blackhole

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"goBlack/common"
	debrid2 "goBlack/pkg/debrid"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Blackhole struct {
	config *common.Config
	deb    debrid2.Service
	cache  *common.Cache
}

func NewBlackhole(config *common.Config, deb debrid2.Service, cache *common.Cache) *Blackhole {
	return &Blackhole{
		config: config,
		deb:    deb,
		cache:  cache,
	}

}

func fileReady(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err) // Returns true if the file exists
}

func checkFileLoop(wg *sync.WaitGroup, dir string, file debrid2.TorrentFile, ready chan<- debrid2.TorrentFile) {
	defer wg.Done()
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()
	path := filepath.Join(dir, file.Path)
	for {
		select {
		case <-ticker.C:
			if fileReady(path) {
				ready <- file
				return
			}
		}
	}
}

func (b *Blackhole) processFiles(arr *debrid2.Arr, torrent *debrid2.Torrent) {
	var wg sync.WaitGroup
	files := torrent.Files
	ready := make(chan debrid2.TorrentFile, len(files))

	log.Printf("Checking %d files...", len(files))

	for _, file := range files {
		wg.Add(1)
		go checkFileLoop(&wg, arr.Debrid.Folder, file, ready)
	}

	go func() {
		wg.Wait()
		close(ready)
	}()

	for r := range ready {
		log.Println("File is ready:", r.Name)
		b.createSymLink(arr, torrent)

	}
	go torrent.Cleanup(true)
	fmt.Printf("%s downloaded", torrent.Name)
}

func (b *Blackhole) createSymLink(arr *debrid2.Arr, torrent *debrid2.Torrent) {
	path := filepath.Join(arr.CompletedFolder, torrent.Folder)
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		log.Printf("Failed to create directory: %s\n", path)
	}

	for _, file := range torrent.Files {
		// Combine the directory and filename to form a full path
		fullPath := filepath.Join(path, file.Name) // completedFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
		// Create a symbolic link if file doesn't exist
		torrentPath := filepath.Join(arr.Debrid.Folder, torrent.Folder, file.Name) // debridFolder/MyTVShow/MyTVShow.S01E01.720p.mkv
		_ = os.Symlink(torrentPath, fullPath)
	}
}

func watcher(watcher *fsnotify.Watcher, events map[string]time.Time) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if filepath.Ext(event.Name) == ".torrent" || filepath.Ext(event.Name) == ".magnet" {
					events[event.Name] = time.Now()
				}

			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("ERROR:", err)
		}
	}
}

func (b *Blackhole) processFilesDebounced(arr *debrid2.Arr, events map[string]time.Time, debouncePeriod time.Duration) {
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()

	for range ticker.C {
		for file, lastEventTime := range events {
			if time.Since(lastEventTime) >= debouncePeriod {
				log.Printf("Torrent file detected: %s", file)
				// Process the torrent file
				torrent, err := b.deb.Process(arr, file)
				if err != nil && torrent != nil {
					// remove torrent file
					torrent.Cleanup(true)
					_ = torrent.MarkAsFailed()
					log.Printf("Error processing torrent file: %s", err)
				}
				if err == nil && torrent != nil && len(torrent.Files) > 0 {
					go b.processFiles(arr, torrent)
				}
				delete(events, file) // remove file from channel

			}
		}
	}
}

func (b *Blackhole) startArr(arr *debrid2.Arr) {
	log.Printf("Watching: %s", arr.WatchFolder)
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Println(err)
	}
	defer func(w *fsnotify.Watcher) {
		err := w.Close()
		if err != nil {
			log.Println(err)
		}
	}(w)
	events := make(map[string]time.Time)

	go watcher(w, events)
	if err = w.Add(arr.WatchFolder); err != nil {
		log.Println("Error Watching folder:", err)
		return
	}

	b.processFilesDebounced(arr, events, 1*time.Second)
}

func (b *Blackhole) Start() {
	log.Println("[*] Starting Blackhole")
	var wg sync.WaitGroup
	for _, conf := range b.config.Arrs {
		wg.Add(1)
		defer wg.Done()
		headers := map[string]string{
			"X-Api-Key": conf.Token,
		}
		client := common.NewRLHTTPClient(nil, headers)

		arr := &debrid2.Arr{
			Debrid:          b.config.Debrid,
			WatchFolder:     conf.WatchFolder,
			CompletedFolder: conf.CompletedFolder,
			Token:           conf.Token,
			URL:             conf.URL,
			Client:          client,
		}
		go b.startArr(arr)
	}
	wg.Wait()
}
