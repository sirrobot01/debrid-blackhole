package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// TestDeleteEntryFilesRefusesCategoryDir is the production repro: an entry whose
// Name is empty (or ".") makes DownloadPath() collapse onto the category
// SavePath. The guard must refuse to RemoveAll that shared directory, leaving
// every sibling entry's data — and the sibling category dirs — intact.
// A valid Name must still remove only its own child directory.
func TestDeleteEntryFilesRefusesCategoryDir(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	store, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	q := newQueue(store, "")
	q.logger = zerolog.Nop()

	downloads := t.TempDir()
	radarr := filepath.Join(downloads, "radarr")
	sonarr := filepath.Join(downloads, "sonarr")
	sibling := filepath.Join(radarr, "OtherMovie") // another entry's symlink dir
	for _, d := range []string{radarr, sonarr, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}
	siblingFile := filepath.Join(sibling, "video.mkv")
	if err := os.WriteFile(siblingFile, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Empty Name: DownloadPath() collapses to the category dir itself.
	empty := &storage.Entry{Protocol: config.ProtocolTorrent, InfoHash: "empty-name-hash", Name: "", SavePath: radarr}
	if filepath.Clean(empty.DownloadPath()) != filepath.Clean(radarr) {
		t.Fatalf("precondition: empty-name DownloadPath %q must equal SavePath %q", empty.DownloadPath(), radarr)
	}
	q.deleteEntryFiles(empty)
	for _, p := range []string{radarr, sonarr, sibling, siblingFile} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("empty-name delete destroyed %q: %v", p, err)
		}
	}

	// "." Name collapses the same way and must also be refused.
	dot := &storage.Entry{Protocol: config.ProtocolTorrent, InfoHash: "dot-name-hash", Name: ".", SavePath: radarr}
	q.deleteEntryFiles(dot)
	if _, err := os.Stat(radarr); err != nil {
		t.Fatalf(`"." delete destroyed the category dir: %v`, err)
	}

	// Control: a valid Name removes only its own child, never the category dir.
	valid := &storage.Entry{Protocol: config.ProtocolTorrent, InfoHash: "valid-hash", Name: "OtherMovie", SavePath: radarr}
	q.deleteEntryFiles(valid)
	if _, err := os.Stat(sibling); !os.IsNotExist(err) {
		t.Fatalf("valid-name delete failed to remove its own child dir (stat err=%v)", err)
	}
	if _, err := os.Stat(radarr); err != nil {
		t.Fatalf("valid-name delete wrongly removed the category dir: %v", err)
	}
}
