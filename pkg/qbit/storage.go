package qbit

import (
	"fmt"
	"github.com/goccy/go-json"
	"os"
	"sort"
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
		torrents = filtered
	}
	return torrents
}

func (ts *TorrentStorage) GetAllSorted(category string, filter string, hashes []string, sortBy string, ascending bool) []*Torrent {
	torrents := ts.GetAll(category, filter, hashes)
	if sortBy != "" {
		sort.Slice(torrents, func(i, j int) bool {
			// If ascending is false, swap i and j to get descending order
			if !ascending {
				i, j = j, i
			}

			switch sortBy {
			case "name":
				return torrents[i].Name < torrents[j].Name
			case "size":
				return torrents[i].Size < torrents[j].Size
			case "added_on":
				return torrents[i].AddedOn < torrents[j].AddedOn
			case "completed":
				return torrents[i].Completed < torrents[j].Completed
			case "progress":
				return torrents[i].Progress < torrents[j].Progress
			case "state":
				return torrents[i].State < torrents[j].State
			case "category":
				return torrents[i].Category < torrents[j].Category
			case "dlspeed":
				return torrents[i].Dlspeed < torrents[j].Dlspeed
			case "upspeed":
				return torrents[i].Upspeed < torrents[j].Upspeed
			case "ratio":
				return torrents[i].Ratio < torrents[j].Ratio
			default:
				// Default sort by added_on
				return torrents[i].AddedOn < torrents[j].AddedOn
			}
		})
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
	return ts.saveToFile()
}

// saveToFile is a helper function to write the current state to the JSON file
func (ts *TorrentStorage) saveToFile() error {
	ts.mu.RLock()
	data, err := json.MarshalIndent(ts.torrents, "", "  ")
	ts.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(ts.filename, data, 0644)
}
