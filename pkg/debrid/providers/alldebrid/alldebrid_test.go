package alldebrid

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/bytedance/sonic"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/internal/request"
)

func TestMagnetsUnmarshalJSON(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []int
	}{
		"array": {
			input: `[{"id":1},{"id":2}]`,
			want:  []int{1, 2},
		},
		"map": {
			input: `{"first":{"id":1}}`,
			want:  []int{1},
		},
		"single object": {
			input: `{"id":1,"filename":"Release.mkv"}`,
			want:  []int{1},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var magnets Magnets
			if err := json.Unmarshal([]byte(tt.input), &magnets); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if len(magnets) != len(tt.want) {
				t.Fatalf("Unmarshal() returned %d magnets, want %d", len(magnets), len(tt.want))
			}
			for i, want := range tt.want {
				if magnets[i].Id != want {
					t.Errorf("magnets[%d].Id = %d, want %d", i, magnets[i].Id, want)
				}
			}
		})
	}
}

func TestGetTorrentSelectsRequestedMagnetFromArray(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "2" {
			t.Errorf("id = %q, want 2", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":1,"filename":"Wrong.mkv","statusCode":1},{"id":2,"filename":"Release.mkv","statusCode":1,"hash":"ABC"}]}}`)
	}))
	t.Cleanup(server.Close)

	ad := &AllDebrid{
		Host:   server.URL,
		client: request.New(request.WithMaxRetries(0)),
		config: config.Debrid{Name: "alldebrid"},
	}
	torrent, err := ad.GetTorrent("2")
	if err != nil {
		t.Fatalf("GetTorrent() error = %v", err)
	}
	if torrent.Id != "2" || torrent.Name != "Release.mkv" || torrent.InfoHash != "ABC" {
		t.Fatalf("GetTorrent() = %#v, want requested magnet 2", torrent)
	}
}

func TestFindMagnetReturnsNotFound(t *testing.T) {
	_, err := findMagnet(Magnets{{Id: 1}}, "2")
	if !errors.Is(err, customerror.TorrentNotFoundError) {
		t.Fatalf("findMagnet() error = %v, want TorrentNotFoundError", err)
	}
}

// torrentLimit caps what a configured `limit` can request; it never lets a
// user push AllDebrid's own ~5000 ceiling higher, and still lets them set a
// lower, self-imposed one.
func TestTorrentLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"unset defaults to the cap", 0, maxTorrentLimit},
		{"negative defaults to the cap", -1, maxTorrentLimit},
		{"below the cap is honoured", 1000, 1000},
		{"exactly at the cap is honoured", maxTorrentLimit, maxTorrentLimit},
		{"above the cap is clamped down", 9000, maxTorrentLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ad := &AllDebrid{config: config.Debrid{Limit: c.limit}}
			if got := ad.torrentLimit(); got != c.want {
				t.Errorf("torrentLimit() with configured limit %d = %d, want %d", c.limit, got, c.want)
			}
		})
	}
}
