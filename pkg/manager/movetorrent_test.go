package manager

import (
	"context"
	"fmt"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	debridCommon "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// fakeMoveClient drives the full Fixer.MoveTorrent path: SubmitMagnet,
// CheckStatus and DeleteTorrent all do real (in-memory) work instead of
// panicking, unlike fakeSlotClient in slot_strategy_test.go, which only
// needs Config and DeleteTorrent. Everything else still panics — this fake
// only ever exercises the re-insertion path.
type fakeMoveClient struct {
	cfg       config.Debrid
	nextID    int
	submitted []string // IDs assigned by SubmitMagnet, in order
	deleted   []string
}

func (f *fakeMoveClient) SubmitMagnet(tr *types.Torrent) (*types.Torrent, error) {
	f.nextID++
	tr.Id = fmt.Sprintf("id-%d", f.nextID)
	tr.Debrid = f.cfg.Name
	f.submitted = append(f.submitted, tr.Id)
	return tr, nil
}

func (f *fakeMoveClient) CheckStatus(tr *types.Torrent) (*types.Torrent, error) {
	tr.Status = types.TorrentStatusDownloaded
	tr.Debrid = f.cfg.Name
	tr.Files = map[string]types.File{
		"movie.mkv": {Name: "movie.mkv", Link: "https://example.invalid/" + tr.Id, Size: 1024},
	}
	return tr, nil
}

func (f *fakeMoveClient) DeleteTorrent(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeMoveClient) GetDownloadLink(string, *types.File) (types.DownloadLink, error) {
	panic("unused")
}
func (f *fakeMoveClient) IsAvailable([]string) map[string]bool      { panic("unused") }
func (f *fakeMoveClient) UpdateTorrent(*types.Torrent) error        { panic("unused") }
func (f *fakeMoveClient) GetTorrent(string) (*types.Torrent, error) { panic("unused") }
func (f *fakeMoveClient) GetTorrents() ([]*types.Torrent, error)    { panic("unused") }
func (f *fakeMoveClient) Config() config.Debrid                     { return f.cfg }
func (f *fakeMoveClient) Logger() zerolog.Logger                    { return zerolog.Nop() }
func (f *fakeMoveClient) RefreshDownloadLinks() error               { panic("unused") }
func (f *fakeMoveClient) CheckFile(context.Context, string, string) error {
	panic("unused")
}
func (f *fakeMoveClient) AccountManager() *account.Manager    { return nil }
func (f *fakeMoveClient) GetProfile() (*types.Profile, error) { panic("unused") }
func (f *fakeMoveClient) GetAvailableSlots() (int, error)     { panic("unused") }
func (f *fakeMoveClient) SyncAccounts()                       {}
func (f *fakeMoveClient) DeleteLink(types.DownloadLink) error { panic("unused") }
func (f *fakeMoveClient) SpeedTest(context.Context) types.SpeedTestResult {
	panic("unused")
}
func (f *fakeMoveClient) SupportsCheck() bool { return false }

var _ debridCommon.Client = (*fakeMoveClient)(nil)

// End-to-end regression test for the remove_after_add / repair interaction:
// drives the real Fixer.MoveTorrent, not applySlotStrategyFor directly, so
// it also guards the one-line call site inside MoveTorrent itself — removing
// that line was confirmed (by hand, while writing the underlying fix) to
// leave the unit-level applySlotStrategyFor tests green, which is exactly
// the gap this test closes.
func TestMoveTorrent_ReappliesSlotStrategyAfterReinsertion(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	strg, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer strg.Close()

	client := &fakeMoveClient{cfg: config.Debrid{
		Name: "alldebrid", Provider: "alldebrid", SlotStrategy: "remove_after_add",
	}}
	m := &Manager{
		storage: strg,
		logger:  zerolog.Nop(),
		clients: xsync.NewMap[string, debridCommon.Client](),
		// Manager.New() always sets this; MoveTorrent reads it directly on
		// some branches (others go through config.Get() instead), so a real
		// Manager never hits a nil pointer here regardless of which variant
		// of that code this test runs against.
		config: &config.Config{},
	}
	m.clients.Store("alldebrid", client)
	f := &Fixer{manager: m}

	entry := &storage.Entry{
		InfoHash: "aaa1",
		Name:     "Test Movie",
		Protocol: config.ProtocolTorrent,
	}

	// First re-insertion: fresh submit, since there is no existing placement.
	success, err := f.MoveTorrent(entry, "alldebrid", true)
	if err != nil || !success {
		t.Fatalf("MoveTorrent (1st) = %v, %v", success, err)
	}
	if len(client.deleted) != 1 || client.deleted[0] != client.submitted[0] {
		t.Fatalf("after 1st insertion: deleted = %v, want [%s]", client.deleted, client.submitted[0])
	}
	pe := entry.Providers["alldebrid"]
	if pe == nil {
		t.Fatal("no placement recorded for alldebrid")
	}
	if pe.RemovedAt == nil {
		t.Fatal("RemovedAt not set after 1st insertion — remove_after_add did not fire")
	}
	if entry.ActiveProvider != "alldebrid" {
		t.Errorf("ActiveProvider = %q, want alldebrid", entry.ActiveProvider)
	}

	// MoveTorrent separately deletes the *previous* placement's ID in a
	// goroutine when a fresh submission gets a different one — unrelated to
	// remove_after_add and pre-existing. Left as ActiveProvider="alldebrid",
	// the 2nd call below would capture oldID="id-1" and schedule that async
	// delete too, racing with this test's synchronous assertions. Clearing it
	// keeps this test isolated to the one thing it's meant to cover.
	entry.ActiveProvider = ""

	// Second re-insertion (a later repair): AddTorrentProvider replaces the
	// placement with a fresh one (RemovedAt nil again). Before the fix this
	// stayed nil forever; the slot must be freed a second time here.
	success, err = f.MoveTorrent(entry, "alldebrid", true)
	if err != nil || !success {
		t.Fatalf("MoveTorrent (2nd) = %v, %v", success, err)
	}
	if len(client.submitted) != 2 {
		t.Fatalf("expected 2 submissions, got %d: %v", len(client.submitted), client.submitted)
	}
	if len(client.deleted) != 2 || client.deleted[1] != client.submitted[1] {
		t.Fatalf("after 2nd insertion: deleted = %v, want [%s %s]", client.deleted, client.submitted[0], client.submitted[1])
	}
	pe = entry.Providers["alldebrid"]
	if pe == nil || pe.RemovedAt == nil {
		t.Fatal("RemovedAt not set after 2nd insertion — the bug this test guards against")
	}

	// The entry was persisted (MoveTorrent's deferred save), not just mutated
	// in memory — confirms applySlotStrategyFor's changes actually survive.
	saved, err := strg.Get(entry.InfoHash)
	if err != nil {
		t.Fatalf("storage.Get: %v", err)
	}
	if saved.Providers["alldebrid"].RemovedAt == nil {
		t.Fatal("RemovedAt not persisted to storage")
	}
}
