package storage

import (
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// ReferencedInfoHashes drives the "no longer referenced" scan: an infohash
// missing from the set is reported as purgeable, so a silent regression here
// would offer live entries for deletion.
func TestReferencedInfoHashes(t *testing.T) {
	// NewStorage builds a logger, which loads the config; point it at a temp
	// directory so the test never touches a real config file.
	config.SetConfigPath(t.TempDir())

	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer s.Close()

	refs := []*ArrFile{
		{ArrName: "tv", ManagedPath: "/media/tv/a/s01e01.mkv", InfoHash: "AABB", FileName: "s01e01.mkv"},
		{ArrName: "tv", ManagedPath: "/media/tv/a/s01e02.mkv", InfoHash: "aabb", FileName: "s01e02.mkv"},
		{ArrName: "movies", ManagedPath: "/media/movies/b/b.mkv", InfoHash: "ccdd", FileName: "b.mkv"},
	}
	for _, r := range refs {
		if err := s.UpsertArrFile(r); err != nil {
			t.Fatalf("UpsertArrFile: %v", err)
		}
	}

	got, err := s.ReferencedInfoHashes()
	if err != nil {
		t.Fatalf("ReferencedInfoHashes: %v", err)
	}

	// Two files of the same torrent collapse to one hash, lowercased so the
	// caller can compare against entry infohashes without worrying about case.
	if len(got) != 2 {
		t.Fatalf("got %d hashes, want 2: %v", len(got), got)
	}
	for _, want := range []string{"aabb", "ccdd"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q", want)
		}
	}

	// Dropping one file of a torrent must keep the torrent referenced.
	if _, err := s.DeleteArrFile("/media/tv/a/s01e01.mkv"); err != nil {
		t.Fatalf("DeleteArrFile: %v", err)
	}
	got, _ = s.ReferencedInfoHashes()
	if _, ok := got["aabb"]; !ok {
		t.Error("torrent lost its reference while a sibling file remains")
	}
}

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
