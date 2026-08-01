package torrin

import (
	"os"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"go.uber.org/ratelimit"
)

func TestLiveSmoke(t *testing.T) {
	key := os.Getenv("TORRIN_TEST_KEY")
	if key == "" {
		t.Skip("set TORRIN_TEST_KEY to run the live smoke test")
	}
	config.SetConfigPath(t.TempDir())

	rl := map[string]ratelimit.Limiter{
		"main":     ratelimit.NewUnlimited(),
		"download": ratelimit.NewUnlimited(),
		"repair":   ratelimit.NewUnlimited(),
	}
	c, err := New(config.Debrid{Name: "torrin", Provider: "torrin", APIKey: key}, rl)
	if err != nil {
		t.Fatal("New:", err)
	}

	p, err := c.GetProfile()
	if err != nil {
		t.Fatal("GetProfile:", err)
	}
	t.Logf("GetProfile OK: email=%s type=%s", p.Email, p.Type)

	torrents, err := c.GetTorrents()
	if err != nil {
		t.Fatal("GetTorrents:", err)
	}
	t.Logf("GetTorrents OK: %d downloaded torrents", len(torrents))
	if len(torrents) == 0 {
		t.Skip("account has no torrents; skipping availability + link checks")
	}

	tr := torrents[0]
	avail := c.IsAvailable([]string{tr.InfoHash})
	t.Logf("IsAvailable(%s) = %v", tr.InfoHash, avail[tr.InfoHash])

	got, err := c.GetTorrent(tr.Id)
	if err != nil {
		t.Fatal("GetTorrent:", err)
	}
	t.Logf("GetTorrent OK: name=%q status=%s files=%d", got.Name, got.Status, len(got.Files))

	files := got.GetFiles()
	if len(files) == 0 {
		t.Skip("torrent has no allowed files; skipping download link")
	}
	f := files[0]
	dl, err := c.GetDownloadLink(got.Id, &f)
	if err != nil {
		t.Fatal("GetDownloadLink:", err)
	}
	u := dl.DownloadLink
	if len(u) > 70 {
		u = u[:70]
	}
	t.Logf("GetDownloadLink OK: %s (%d bytes) -> %s...", dl.Filename, dl.Size, u)
	if dl.DownloadLink == "" {
		t.Fatal("empty download link")
	}
}
