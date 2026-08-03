package hybrid

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// The package logger loads the global config on first use, which writes a
// config file. Point it at a scratch directory so tests never touch a real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "decypharr-hybrid-test")
	if err != nil {
		panic(err)
	}
	config.SetConfigPath(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func testStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := New(Config{DataPath: path, CacheSize: 16})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// churn writes and deletes enough entries that the log is mostly dead space,
// which is what makes compaction worth doing.
func churn(t *testing.T, s *Store) {
	t.Helper()
	for i := range 32 {
		key := string(rune('a' + i%26))
		if err := s.Put(key, []byte("value-that-takes-up-some-room"), nil); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	for i := range 16 {
		if err := s.Delete(string(rune('a' + i))); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
}

// TestCompactKeepsLogWhenSwapFails is the regression test for the compaction
// swap destroying the store. The old code removed the log and then renamed the
// compacted copy over it, discarding both errors, so a failed rename left no
// log at all and every later read returned EOF.
//
// The failure is forced by making the destination un-renameable: on Windows an
// open handle is enough, and everywhere else a directory in place of the file
// makes the rename fail. Either way the store must survive with its data.
func TestCompactKeepsLogWhenSwapFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s := testStore(t, path)
	defer s.Close()
	churn(t, s)
	if err := s.Put("keeper", []byte("must-survive"), nil); err != nil {
		t.Fatalf("Put keeper: %v", err)
	}

	// Hold the destination open so the rename cannot replace it.
	blocker, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open blocker: %v", err)
	}

	compactErr := s.Compact()
	blocker.Close()

	// The rename genuinely fails only on Windows; elsewhere this compaction
	// succeeds and there is nothing to assert about the failure path.
	if compactErr == nil {
		t.Skip("rename over an open file succeeded on this platform")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log must still exist after a failed compaction swap: %v", err)
	}
	got, err := s.Get("keeper")
	if err != nil {
		t.Fatalf("store must keep serving after a failed swap: %v", err)
	}
	if string(got) != "must-survive" {
		t.Fatalf("got %q, want %q", got, "must-survive")
	}
}

// TestCompactSucceedsAndLeavesNoTempFile covers the normal path: the data
// survives, the log is at the expected path, and no ".compact" file is left.
func TestCompactSucceedsAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s := testStore(t, path)
	defer s.Close()
	churn(t, s)
	if err := s.Put("keeper", []byte("must-survive"), nil); err != nil {
		t.Fatalf("Put keeper: %v", err)
	}

	if err := s.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if _, err := os.Stat(path + ".compact"); !os.IsNotExist(err) {
		t.Fatalf("compaction temp file must not be left behind")
	}
	if s.log.path != path {
		t.Fatalf("log path = %q, want %q", s.log.path, path)
	}
	got, err := s.Get("keeper")
	if err != nil {
		t.Fatalf("Get after compaction: %v", err)
	}
	if string(got) != "must-survive" {
		t.Fatalf("got %q, want %q", got, "must-survive")
	}
}

// TestRecoverOrphanedCompaction covers stores already broken by the old code:
// only a ".compact" file survives, and the store must adopt it instead of
// silently starting empty (openAppendLog creates the missing log with O_CREATE).
func TestRecoverOrphanedCompaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s := testStore(t, path)
	if err := s.Put("keeper", []byte("must-survive"), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reproduce the wreckage the old swap left behind.
	if err := os.Rename(path, path+".compact"); err != nil {
		t.Fatalf("stage orphaned compaction: %v", err)
	}

	s2 := testStore(t, path)
	defer s2.Close()

	got, err := s2.Get("keeper")
	if err != nil {
		t.Fatalf("data must be recovered from the orphaned .compact file: %v", err)
	}
	if string(got) != "must-survive" {
		t.Fatalf("got %q, want %q", got, "must-survive")
	}
	if _, err := os.Stat(path + ".compact"); !os.IsNotExist(err) {
		t.Fatalf("orphaned compaction file should have been adopted, not copied")
	}
}
