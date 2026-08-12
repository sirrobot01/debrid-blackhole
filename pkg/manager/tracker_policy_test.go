package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	debridCommon "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// A magnet with a couple of tracker params, used to verify whether a
// submission strips them.
const magnetWithTrackers = "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
	"&dn=Test+Movie&tr=http%3A%2F%2Ftracker.example%2Fannounce&tr=udp%3A%2F%2Ftracker2.example%3A1337"

// newTorrentQueueEntry copies the per-request tracker-stripping choice onto the
// entry it builds — without this, the choice only lived on the short-lived
// ImportRequest and was lost the moment the entry was persisted.
func TestNewTorrentQueueEntry_PersistsRmTrackerUrls(t *testing.T) {
	a := arr.New("radarr", "http://radarr:7878", "token", false, nil, "", "manual")

	for _, want := range []bool{true, false} {
		req := &ImportRequest{
			Arr:           a,
			RmTrackerUrls: want,
			Magnet: &utils.Magnet{
				InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Name:     "Test Movie",
			},
		}
		entry := newTorrentQueueEntry(req, types.TorrentStatusQueued)
		if entry.RmTrackerUrls != want {
			t.Errorf("RmTrackerUrls = %v, want %v", entry.RmTrackerUrls, want)
		}
	}
}

// fakeTrackerPolicyClient captures the magnet link MoveTorrent actually submits,
// so tests can check whether trackers were stripped.
type fakeTrackerPolicyClient struct {
	cfg       config.Debrid
	submitted []string // magnet links passed to SubmitMagnet, in order
}

func (f *fakeTrackerPolicyClient) SubmitMagnet(tr *types.Torrent) (*types.Torrent, error) {
	f.submitted = append(f.submitted, tr.Magnet.Link)
	tr.Id = "id-1"
	tr.Debrid = f.cfg.Name
	return tr, nil
}
func (f *fakeTrackerPolicyClient) CheckStatus(tr *types.Torrent) (*types.Torrent, error) {
	tr.Status = types.TorrentStatusDownloaded
	tr.Files = map[string]types.File{"movie.mkv": {Name: "movie.mkv", Link: "https://example.invalid/id-1", Size: 1024}}
	return tr, nil
}
func (f *fakeTrackerPolicyClient) DeleteTorrent(string) error { return nil }
func (f *fakeTrackerPolicyClient) GetDownloadLink(string, *types.File) (types.DownloadLink, error) {
	panic("unused")
}
func (f *fakeTrackerPolicyClient) IsAvailable([]string) map[string]bool      { panic("unused") }
func (f *fakeTrackerPolicyClient) UpdateTorrent(*types.Torrent) error        { panic("unused") }
func (f *fakeTrackerPolicyClient) GetTorrent(string) (*types.Torrent, error) { panic("unused") }
func (f *fakeTrackerPolicyClient) GetTorrents() ([]*types.Torrent, error)    { panic("unused") }
func (f *fakeTrackerPolicyClient) Config() config.Debrid                     { return f.cfg }
func (f *fakeTrackerPolicyClient) Logger() zerolog.Logger                    { return zerolog.Nop() }
func (f *fakeTrackerPolicyClient) RefreshDownloadLinks() error               { panic("unused") }
func (f *fakeTrackerPolicyClient) CheckFile(context.Context, string, string) error {
	panic("unused")
}
func (f *fakeTrackerPolicyClient) AccountManager() *account.Manager    { return nil }
func (f *fakeTrackerPolicyClient) GetProfile() (*types.Profile, error) { panic("unused") }
func (f *fakeTrackerPolicyClient) GetAvailableSlots() (int, error)     { panic("unused") }
func (f *fakeTrackerPolicyClient) SyncAccounts()                       {}
func (f *fakeTrackerPolicyClient) DeleteLink(types.DownloadLink) error { panic("unused") }
func (f *fakeTrackerPolicyClient) SpeedTest(context.Context) types.SpeedTestResult {
	panic("unused")
}
func (f *fakeTrackerPolicyClient) SupportsCheck() bool { return false }

var _ debridCommon.Client = (*fakeTrackerPolicyClient)(nil)

// The entry's own RmTrackerUrls must be honoured on reinsertion even when the
// current global always_rm_tracker_urls setting is off — otherwise a torrent
// added with "remove tracker URLs" checked loses that choice the moment it
// needs to be reinserted (repair, managed-only auto-reinsertion, etc).
func TestMoveTorrent_HonoursEntryRmTrackerUrls(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	config.SetConfigPath(t.TempDir())
	if config.Get().AlwaysRmTrackerUrls {
		t.Fatal("setup: expected AlwaysRmTrackerUrls to default to false")
	}

	strg, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer strg.Close()

	client := &fakeTrackerPolicyClient{cfg: config.Debrid{Name: "realdebrid", Provider: "realdebrid"}}
	m := &Manager{
		storage: strg,
		logger:  zerolog.Nop(),
		clients: xsync.NewMap[string, debridCommon.Client](),
		config:  &config.Config{},
	}
	m.clients.Store("realdebrid", client)
	f := &Fixer{manager: m}

	entry := &storage.Entry{
		InfoHash:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:          "Test Movie",
		Protocol:      config.ProtocolTorrent,
		Magnet:        magnetWithTrackers,
		RmTrackerUrls: true,
	}

	success, err := f.MoveTorrent(entry, "realdebrid", true)
	if err != nil || !success {
		t.Fatalf("MoveTorrent = %v, %v", success, err)
	}
	if len(client.submitted) != 1 {
		t.Fatalf("submitted = %v, want exactly one submission", client.submitted)
	}
	if strings.Contains(client.submitted[0], "tracker.example") || strings.Contains(client.submitted[0], "tracker2.example") {
		t.Errorf("submitted magnet still has trackers despite entry.RmTrackerUrls=true: %s", client.submitted[0])
	}
}
