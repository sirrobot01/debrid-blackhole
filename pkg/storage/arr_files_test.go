package storage

import (
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// FindArrFilesByFolder feeds SeriesDelete/MovieDelete cleanup: whatever it returns for one
// ARR gets deleted from the debrid provider when allow_delete is on. If it isn't scoped to
// the calling ARR, two ARRs with overlapping library roots (shared parent folder, symlinked
// structure) let one ARR's delete event sweep up and delete the other ARR's tracked files —
// media it has no knowledge of or authority over.
func TestFindArrFilesByFolder_ScopedToArr(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	config.SetConfigPath(t.TempDir())

	strg, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer strg.Close()

	sonarrFile := &ArrFile{
		ArrName:     "sonarr",
		ManagedPath: "/data/media/tv/Show/episode.mkv",
		InfoHash:    "aaa1",
		FileName:    "episode.mkv",
	}
	radarrFile := &ArrFile{
		ArrName:     "radarr",
		ManagedPath: "/data/media/tv/Movie/movie.mkv", // overlapping root by coincidence
		InfoHash:    "bbb2",
		FileName:    "movie.mkv",
	}
	if err := strg.UpsertArrFile(sonarrFile); err != nil {
		t.Fatalf("UpsertArrFile(sonarr): %v", err)
	}
	if err := strg.UpsertArrFile(radarrFile); err != nil {
		t.Fatalf("UpsertArrFile(radarr): %v", err)
	}

	// Sonarr reports a SeriesDelete for the shared parent folder — only its own
	// file should come back, never radarr's.
	refs, err := strg.FindArrFilesByFolder("sonarr", "/data/media/tv")
	if err != nil {
		t.Fatalf("FindArrFilesByFolder: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %v, want exactly 1 (sonarr's own file)", refs)
	}
	if refs[0].ArrName != "sonarr" || refs[0].InfoHash != "aaa1" {
		t.Errorf("refs[0] = %+v, want sonarr's file (aaa1)", refs[0])
	}

	// Same folder, radarr's turn — must only get its own file, not sonarr's.
	refs, err = strg.FindArrFilesByFolder("radarr", "/data/media/tv")
	if err != nil {
		t.Fatalf("FindArrFilesByFolder: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %v, want exactly 1 (radarr's own file)", refs)
	}
	if refs[0].ArrName != "radarr" || refs[0].InfoHash != "bbb2" {
		t.Errorf("refs[0] = %+v, want radarr's file (bbb2)", refs[0])
	}

	// An ARR name that owns nothing under the folder gets nothing back.
	refs, err = strg.FindArrFilesByFolder("lidarr", "/data/media/tv")
	if err != nil {
		t.Fatalf("FindArrFilesByFolder: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("refs = %v, want none for an unrelated ARR", refs)
	}
}
