package manager

import (
	"context"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	debridCommon "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// fakeSlotClient is a minimal common.Client double. Only DeleteTorrent and
// Config are exercised by applySlotStrategyFor; everything else panics if
// called, so an accidental new dependency fails loudly instead of silently
// returning a zero value.
type fakeSlotClient struct {
	cfg     config.Debrid
	deleted []string
}

func (f *fakeSlotClient) SubmitMagnet(*types.Torrent) (*types.Torrent, error) { panic("unused") }
func (f *fakeSlotClient) CheckStatus(*types.Torrent) (*types.Torrent, error)  { panic("unused") }
func (f *fakeSlotClient) GetDownloadLink(string, *types.File) (types.DownloadLink, error) {
	panic("unused")
}
func (f *fakeSlotClient) DeleteTorrent(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeSlotClient) IsAvailable([]string) map[string]bool      { panic("unused") }
func (f *fakeSlotClient) UpdateTorrent(*types.Torrent) error        { panic("unused") }
func (f *fakeSlotClient) GetTorrent(string) (*types.Torrent, error) { panic("unused") }
func (f *fakeSlotClient) GetTorrents() ([]*types.Torrent, error)    { panic("unused") }
func (f *fakeSlotClient) Config() config.Debrid                     { return f.cfg }
func (f *fakeSlotClient) Logger() zerolog.Logger                    { return zerolog.Nop() }
func (f *fakeSlotClient) RefreshDownloadLinks() error               { panic("unused") }
func (f *fakeSlotClient) CheckFile(context.Context, string, string) error {
	panic("unused")
}
func (f *fakeSlotClient) AccountManager() *account.Manager    { return nil }
func (f *fakeSlotClient) GetProfile() (*types.Profile, error) { panic("unused") }
func (f *fakeSlotClient) GetAvailableSlots() (int, error)     { panic("unused") }
func (f *fakeSlotClient) SyncAccounts()                       {}
func (f *fakeSlotClient) DeleteLink(types.DownloadLink) error { panic("unused") }
func (f *fakeSlotClient) SpeedTest(context.Context) types.SpeedTestResult {
	panic("unused")
}
func (f *fakeSlotClient) SupportsCheck() bool { return false }

var _ debridCommon.Client = (*fakeSlotClient)(nil)

func newSlotStrategyManager(t *testing.T, strategy string) (*Manager, *fakeSlotClient) {
	t.Helper()
	client := &fakeSlotClient{cfg: config.Debrid{
		Name: "alldebrid", Provider: "alldebrid", SlotStrategy: strategy,
	}}
	m := &Manager{
		logger:  zerolog.Nop(),
		clients: xsync.NewMap[string, debridCommon.Client](),
	}
	m.clients.Store("alldebrid", client)
	return m, client
}

// This is the exact scenario that used to leave a repaired torrent on
// AllDebrid forever: entry.AddTorrentProvider (what MoveTorrent calls on a
// successful re-insertion) resets RemovedAt to nil on the fresh placement,
// and nothing used to re-apply remove_after_add afterward.
func TestApplySlotStrategyFor_ReappliesAfterReinsertion(t *testing.T) {
	m, client := newSlotStrategyManager(t, "remove_after_add")

	entry := &storage.Entry{InfoHash: "aaa1", Name: "test"}
	entry.AddTorrentProvider(&types.Torrent{Id: "111", Debrid: "alldebrid"})

	m.applySlotStrategyFor(entry, "alldebrid")
	if len(client.deleted) != 1 || client.deleted[0] != "111" {
		t.Fatalf("first free: deleted = %v, want [111]", client.deleted)
	}
	if entry.Providers["alldebrid"].RemovedAt == nil {
		t.Fatal("RemovedAt not set after first free")
	}

	// A second call before any re-insertion must not delete again — the
	// entry is already off AllDebrid.
	m.applySlotStrategyFor(entry, "alldebrid")
	if len(client.deleted) != 1 {
		t.Fatalf("re-applying without reinsertion deleted again: %v", client.deleted)
	}

	// Simulate MoveTorrent's re-insertion: a fresh placement, RemovedAt unset.
	entry.AddTorrentProvider(&types.Torrent{Id: "222", Debrid: "alldebrid"})
	if entry.Providers["alldebrid"].RemovedAt != nil {
		t.Fatal("test setup: AddTorrentProvider should reset RemovedAt")
	}

	m.applySlotStrategyFor(entry, "alldebrid")
	if len(client.deleted) != 2 || client.deleted[1] != "222" {
		t.Fatalf("after reinsertion: deleted = %v, want [111 222]", client.deleted)
	}
	if entry.Providers["alldebrid"].RemovedAt == nil {
		t.Fatal("RemovedAt not set after the slot was freed again post-reinsertion")
	}
}

func TestApplySlotStrategyFor_IgnoresOtherStrategies(t *testing.T) {
	for _, strategy := range []string{"", "remove_oldest"} {
		m, client := newSlotStrategyManager(t, strategy)
		entry := &storage.Entry{InfoHash: "bbb2", Name: "test"}
		entry.AddTorrentProvider(&types.Torrent{Id: "333", Debrid: "alldebrid"})

		m.applySlotStrategyFor(entry, "alldebrid")
		if len(client.deleted) != 0 {
			t.Errorf("strategy %q: deleted = %v, want none", strategy, client.deleted)
		}
		if entry.Providers["alldebrid"].RemovedAt != nil {
			t.Errorf("strategy %q: RemovedAt set, want untouched", strategy)
		}
	}
}
