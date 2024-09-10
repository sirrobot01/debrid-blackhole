package qbit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"goBlack/pkg/debrid"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func generateSID() (string, error) {
	bytes := make([]byte, sidLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func JSONResponse(w http.ResponseWriter, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func fileReady(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err) // Returns true if the file exists
}

func checkFileLoop(wg *sync.WaitGroup, dir string, file debrid.TorrentFile, ready chan<- debrid.TorrentFile) {
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
