package shared

import (
	"encoding/json"
	"os"
	"sync"
)

type TorrentStorage struct {
	torrents map[string]*Torrent
	mu       sync.RWMutex
	order    []string
}

func loadTorrentsFromJSON(filename string) (map[string]*Torrent, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	torrents := make(map[string]*Torrent)
	if err := json.Unmarshal(data, &torrents); err != nil {
		return nil, err
	}
	return torrents, nil
}

func NewTorrentStorage(filename string) *TorrentStorage {
	// Open the json file and read the data
	torrents, err := loadTorrentsFromJSON(filename)
	if err != nil {
		torrents = make(map[string]*Torrent)
	}
	order := make([]string, 0, len(torrents))
	for id := range torrents {
		order = append(order, id)
	}
	// Create a new TorrentStorage
	return &TorrentStorage{
		torrents: torrents,
		order:    order,
	}
}

func (ts *TorrentStorage) Add(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.torrents[torrent.Hash] = torrent
	ts.order = append(ts.order, torrent.Hash)
}

func (ts *TorrentStorage) AddOrUpdate(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, exists := ts.torrents[torrent.Hash]; !exists {
		ts.order = append(ts.order, torrent.Hash)
	}
	ts.torrents[torrent.Hash] = torrent
}

func (ts *TorrentStorage) GetByID(id string) *Torrent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	for _, torrent := range ts.torrents {
		if torrent.ID == id {
			return torrent
		}
	}
	return nil
}

func (ts *TorrentStorage) Get(hash string) *Torrent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.torrents[hash]
}

func (ts *TorrentStorage) GetAll(category string, filter string, hashes []string) []*Torrent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	torrents := make([]*Torrent, 0)
	for _, id := range ts.order {
		torrent := ts.torrents[id]
		if category != "" && torrent.Category != category {
			continue
		}
		if filter != "" && torrent.State != filter {
			continue
		}
		torrents = append(torrents, torrent)
	}
	if len(hashes) > 0 {
		filtered := make([]*Torrent, 0, len(torrents))
		for _, hash := range hashes {
			if torrent := ts.torrents[hash]; torrent != nil {
				filtered = append(filtered, torrent)
			}
		}
		torrents = filtered
	}
	return torrents
}

func (ts *TorrentStorage) Update(torrent *Torrent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.torrents[torrent.Hash] = torrent
}

func (ts *TorrentStorage) Delete(hash string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	torrent, exists := ts.torrents[hash]
	if !exists {
		return
	}
	delete(ts.torrents, hash)
	for i, id := range ts.order {
		if id == hash {
			ts.order = append(ts.order[:i], ts.order[i+1:]...)
			break
		}
	}
	// Delete the torrent folder
	if torrent.ContentPath != "" {
		err := os.RemoveAll(torrent.ContentPath)
		if err != nil {
			return
		}
	}
}

func (ts *TorrentStorage) Save(filename string) error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	data, err := json.Marshal(ts.torrents)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}
