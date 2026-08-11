package storage

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const arrFilesVersion = "1"

// ArrFile tracks a managed path (Sonarr/Radarr library file) and its Decypharr source.
type ArrFile struct {
	ArrName     string    `json:"arr_name"`
	ManagedPath string    `json:"managed_path"`
	InfoHash    string    `json:"info_hash"`
	FileName    string    `json:"file_name"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// normalizePath returns a canonical, lowercase, cleaned path suitable as a store key.
func normalizePath(p string) string {
	return filepath.Clean(strings.ToLower(p))
}

// initArrFilesStore writes the version sentinel on first startup.
// Future schema migrations can be added here by comparing string(data) to arrFilesVersion.
func (s *Storage) initArrFilesStore() {
	data, _ := s.arrFiles.Get("__version__")
	if len(data) == 0 {
		_ = s.arrFiles.Put("__version__", []byte(arrFilesVersion), nil)
	}
	// future: if string(data) != arrFilesVersion → apply migration
}

// UpsertArrFile stores or updates an arr file record keyed by ref.ManagedPath.
func (s *Storage) UpsertArrFile(ref *ArrFile) error {
	if ref == nil {
		return fmt.Errorf("ref is nil")
	}
	ref.UpdatedAt = time.Now()
	key := normalizePath(ref.ManagedPath)
	data, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	return s.arrFiles.Put(key, data, nil)
}

// GetArrFile retrieves an arr file record by its managed path. Returns nil, nil when not found.
func (s *Storage) GetArrFile(managedPath string) (*ArrFile, error) {
	key := normalizePath(managedPath)
	data, err := s.arrFiles.Get(key)
	if err != nil {
		return nil, nil
	}
	var ref ArrFile
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, err
	}
	return &ref, nil
}

// DeleteArrFile removes the arr file record for managedPath and returns it (nil if absent).
func (s *Storage) DeleteArrFile(managedPath string) (*ArrFile, error) {
	ref, err := s.GetArrFile(managedPath)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil
	}
	key := normalizePath(managedPath)
	if err := s.arrFiles.Delete(key); err != nil {
		return nil, err
	}
	return ref, nil
}

// FindArrFilesByInfoHash returns all arr file records that share the same infohash.
// Used to decide whether a torrent entry is still referenced by any ARR after a deletion.
func (s *Storage) FindArrFilesByInfoHash(infohash string) ([]ArrFile, error) {
	var refs []ArrFile
	_ = s.arrFiles.ForEach(func(_ string, value []byte) error {
		var ref ArrFile
		if err := json.Unmarshal(value, &ref); err != nil {
			return nil
		}
		if strings.EqualFold(ref.InfoHash, infohash) {
			refs = append(refs, ref)
		}
		return nil
	})
	return refs, nil
}

// GetArrFilesLastEventDate returns the date of the last processed history event for the given ARR.
// Returns zero time and false when no date has been stored yet (first bootstrap).
func (s *Storage) GetArrFilesLastEventDate(arrName string) (time.Time, bool) {
	key := "__last_event__" + strings.ToLower(arrName)
	data, err := s.arrFiles.Get(key)
	if err != nil || len(data) == 0 {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, string(data))
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// SetArrFilesLastEventDate stores the date of the last processed history event for the given ARR.
func (s *Storage) SetArrFilesLastEventDate(arrName string, t time.Time) error {
	key := "__last_event__" + strings.ToLower(arrName)
	return s.arrFiles.Put(key, []byte(t.UTC().Format(time.RFC3339Nano)), nil)
}

// FindArrFilesByFolder returns all arr file records for arrName whose ManagedPath is nested
// under folderPath. Scoped to arrName so a SeriesDelete/MovieDelete from one ARR can't sweep
// up another ARR's tracked files when their library roots overlap (shared parent folder,
// symlinked structure, etc) — each ARR only ever reports its own folder in these events, so
// nothing outside it should be touched by that request.
// Used for SeriesDelete / MovieDelete events where only a root folder is provided.
func (s *Storage) FindArrFilesByFolder(arrName, folderPath string) ([]ArrFile, error) {
	prefix := normalizePath(folderPath) + string(filepath.Separator)
	var refs []ArrFile
	_ = s.arrFiles.ForEach(func(_ string, value []byte) error {
		var ref ArrFile
		if err := json.Unmarshal(value, &ref); err != nil {
			return nil
		}
		if ref.ArrName == arrName && strings.HasPrefix(normalizePath(ref.ManagedPath), prefix) {
			refs = append(refs, ref)
		}
		return nil
	})
	return refs, nil
}
