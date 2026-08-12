package manager

import (
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// GetUnmanagedEntries decides what the local purge deletes. It reads only
// arr_refs and the active queue — a category on the entry has no effect,
// which is easy to reintroduce by accident (it happened twice already in
// this file's history) without a test to catch it.
func TestGetUnmanagedEntries(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	strg, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer strg.Close()

	m := &Manager{
		storage: strg,
		queue:   newQueue(strg, ""),
	}

	// Referenced by an arr: never purgeable.
	referenced := &storage.Entry{InfoHash: "aaa1", Name: "referenced", Category: "sonarr"}
	if err := strg.AddOrUpdate(referenced); err != nil {
		t.Fatalf("AddOrUpdate(referenced): %v", err)
	}
	if err := strg.UpsertArrFile(&storage.ArrFile{
		ArrName: "sonarr", ManagedPath: "/media/referenced", InfoHash: "aaa1", FileName: "referenced.mkv",
	}); err != nil {
		t.Fatalf("UpsertArrFile: %v", err)
	}

	// Not referenced, no category: the "never imported through an arr" case.
	unreferenced := &storage.Entry{InfoHash: "bbb2", Name: "unreferenced"}
	if err := strg.AddOrUpdate(unreferenced); err != nil {
		t.Fatalf("AddOrUpdate(unreferenced): %v", err)
	}

	// Not referenced, actively downloading: excluded regardless of arr_refs.
	queued := &storage.Entry{InfoHash: "ccc3", Name: "queued"}
	if err := strg.AddOrUpdate(queued); err != nil {
		t.Fatalf("AddOrUpdate(queued): %v", err)
	}
	if err := m.queue.Add(queued); err != nil {
		t.Fatalf("queue.Add: %v", err)
	}

	// Not referenced, custom category: category must not exempt it.
	tagged := &storage.Entry{InfoHash: "ddd4", Name: "tagged", Category: "manual"}
	if err := strg.AddOrUpdate(tagged); err != nil {
		t.Fatalf("AddOrUpdate(tagged): %v", err)
	}

	got, err := m.GetUnmanagedEntries()
	if err != nil {
		t.Fatalf("GetUnmanagedEntries: %v", err)
	}

	gotHashes := make(map[string]bool, len(got))
	for _, e := range got {
		gotHashes[e.InfoHash] = true
	}

	want := map[string]bool{"bbb2": true, "ddd4": true}
	if len(gotHashes) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(gotHashes), len(want), gotHashes)
	}
	for hash := range want {
		if !gotHashes[hash] {
			t.Errorf("missing %q from purgeable set", hash)
		}
	}
	for hash := range gotHashes {
		if !want[hash] {
			t.Errorf("unexpected %q in purgeable set", hash)
		}
	}
}
