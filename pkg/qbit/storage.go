package qbit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

func keyPair(hash, category string) string {
	if category == "" {
		category = "uncategorized"
	}
	return fmt.Sprintf("%s|%s", hash, category)
}

type Torrents = map[string]*Torrent

type TorrentStorage struct {
	torrents Torrents
	mu       sync.RWMutex
	filename string // Added to store the filename for persistence
}

func loadTorrentsFromJSON(filename string) (Torrents, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	torrents := make(Torrents)
	if err := json.Unmarshal(data, &torrents); err != nil {
		return nil, err
	}
	return torrents, nil
}

func NewTorrentStorage(filename string) *TorrentStorage {
	// Open the JSON file and read the data
	torrents, err := loadTorrentsFromJSON(filename)
	if err != nil {
		torrents = make(Torrents)
	}
	// Create a new TorrentStorage
	return &TorrentStorage{
		torrents: torrents,
		filename: filename,
	}
}

func (ts *TorrentStorage) Add(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.torrents[keyPair(torrent.Hash, torrent.Category)] = torrent
	go func() {
		err := ts.saveToFile()
		if err != nil {
			fmt.Println(err)
		}
	}()
}

func (ts *TorrentStorage) AddOrUpdate(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.torrents[keyPair(torrent.Hash, torrent.Category)] = torrent
	go func() {
		err := ts.saveToFile()
		if err != nil {
			fmt.Println(err)
		}
	}()
}

func (ts *TorrentStorage) Get(hash, category string) *Torrent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	torrent, exists := ts.torrents[keyPair(hash, category)]
	if !exists && category == "" {
		// Try to find the torrent without knowing the category
		for _, t := range ts.torrents {
			if t.Hash == hash {
				return t
			}
		}
	}
	return torrent
}

func (ts *TorrentStorage) GetAll(category string, filter string, hashes []string) []*Torrent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	torrents := make([]*Torrent, 0)
	for _, torrent := range ts.torrents {
		if category != "" && torrent.Category != category {
			continue
		}
		if filter != "" && torrent.State != filter {
			continue
		}
		torrents = append(torrents, torrent)
	}

	if len(hashes) > 0 {
		filtered := make([]*Torrent, 0)
		for _, hash := range hashes {
			for _, torrent := range torrents {
				if torrent.Hash == hash {
					filtered = append(filtered, torrent)
				}
			}
		}
		return filtered
	}
	return torrents
}

func (ts *TorrentStorage) Update(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.torrents[keyPair(torrent.Hash, torrent.Category)] = torrent
	go func() {
		err := ts.saveToFile()
		if err != nil {
			fmt.Println(err)
		}
	}()
}

func (ts *TorrentStorage) Delete(hash, category string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	key := keyPair(hash, category)
	torrent, exists := ts.torrents[key]
	if !exists && category == "" {
		// Remove the torrent without knowing the category
		for k, t := range ts.torrents {
			if t.Hash == hash {
				key = k
				torrent = t
				break
			}
		}
	}
	delete(ts.torrents, key)
	if torrent == nil {
		return
	}
	// Delete the torrent folder
	if torrent.ContentPath != "" {
		err := os.RemoveAll(torrent.ContentPath)
		if err != nil {
			return
		}
	}
	go func() {
		err := ts.saveToFile()
		if err != nil {
			fmt.Println(err)
		}
	}()
}

func (ts *TorrentStorage) DeleteMultiple(hashes []string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, hash := range hashes {
		for key, torrent := range ts.torrents {
			if torrent.Hash == hash {
				delete(ts.torrents, key)
			}
		}
	}
	go func() {
		err := ts.saveToFile()
		if err != nil {
			fmt.Println(err)
		}
	}()
}

func (ts *TorrentStorage) Save() error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.saveToFile()
}

// saveToFile is a helper function to write the current state to the JSON file
func (ts *TorrentStorage) saveToFile() error {
	data, err := json.MarshalIndent(ts.torrents, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ts.filename, data, 0644)
}
