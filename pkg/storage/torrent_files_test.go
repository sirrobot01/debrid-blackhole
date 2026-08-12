package storage

import (
	"os"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// The reinsertion path (fixer.go) treats a LoadTorrentFile error as "no stored
// file, fall back to magnet" rather than a hard failure — so a round-trip bug
// here wouldn't crash anything, it would just silently degrade every
// reinsertion back to magnet-only. That makes this round-trip worth pinning
// down directly rather than relying on it to surface elsewhere.
func TestTorrentFileRoundTrip(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	data := []byte("d8:announce...fake bencoded torrent bytes...e")
	infohash := "AABBCCDD"

	if err := SaveTorrentFile(infohash, data); err != nil {
		t.Fatalf("SaveTorrentFile: %v", err)
	}

	got, err := LoadTorrentFile(infohash)
	if err != nil {
		t.Fatalf("LoadTorrentFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("LoadTorrentFile = %q, want %q", got, data)
	}

	if err := DeleteTorrentFile(infohash); err != nil {
		t.Fatalf("DeleteTorrentFile: %v", err)
	}
	if _, err := LoadTorrentFile(infohash); err == nil {
		t.Error("LoadTorrentFile succeeded after DeleteTorrentFile, want an error")
	}
}

// SaveTorrentFile is called unconditionally on every submission regardless of
// whether a .torrent file was actually available (e.g. a magnet-only add) —
// it must no-op rather than write an empty/garbage file that LoadTorrentFile
// would later "succeed" on with zero useful bytes.
func TestSaveTorrentFile_EmptyDataNoops(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	if err := SaveTorrentFile("some-hash", nil); err != nil {
		t.Fatalf("SaveTorrentFile(nil): %v", err)
	}
	if _, err := os.Stat(torrentPath("some-hash")); !os.IsNotExist(err) {
		t.Errorf("expected no file to be written for empty data, stat err = %v", err)
	}

	if err := SaveTorrentFile("", []byte("data")); err != nil {
		t.Fatalf("SaveTorrentFile(empty infohash): %v", err)
	}
	if _, err := os.Stat(torrentPath("")); !os.IsNotExist(err) {
		t.Errorf("expected no file to be written for empty infohash, stat err = %v", err)
	}
}

// DeleteTorrentFile is called from Storage.Delete on every entry removal, most
// of which never had a stored .torrent file in the first place — it must treat
// "nothing to delete" as success, not bubble up os.ErrNotExist and abort the
// entry deletion (see pkg/storage/entry.go's Delete, which returns early on
// DeleteTorrentFile's error).
func TestDeleteTorrentFile_MissingFileIsNotAnError(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	if err := DeleteTorrentFile("never-saved"); err != nil {
		t.Errorf("DeleteTorrentFile on a never-saved infohash = %v, want nil", err)
	}
}
