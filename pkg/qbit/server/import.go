package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"goBlack/common"
	"goBlack/pkg/arr"
	"goBlack/pkg/debrid"
	"sync"
	"time"
)

type ImportRequest struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	URI       string   `json:"uri"`
	Arr       *arr.Arr `json:"arr"`
	IsSymlink bool     `json:"isSymlink"`
	SeriesId  int      `json:"series"`
	Seasons   []int    `json:"seasons"`
	Episodes  []string `json:"episodes"`

	Failed      bool      `json:"failed"`
	FailedAt    time.Time `json:"failedAt"`
	Reason      string    `json:"reason"`
	Completed   bool      `json:"completed"`
	CompletedAt time.Time `json:"completedAt"`
	Async       bool      `json:"async"`
}

type ManualImportResponseSchema struct {
	Priority            string    `json:"priority"`
	Status              string    `json:"status"`
	Result              string    `json:"result"`
	Queued              time.Time `json:"queued"`
	Trigger             string    `json:"trigger"`
	SendUpdatesToClient bool      `json:"sendUpdatesToClient"`
	UpdateScheduledTask bool      `json:"updateScheduledTask"`
	Id                  int       `json:"id"`
}

func NewImportRequest(uri string, arr *arr.Arr, isSymlink bool) *ImportRequest {
	return &ImportRequest{
		ID:        uuid.NewString(),
		URI:       uri,
		Arr:       arr,
		Failed:    false,
		Completed: false,
		Async:     false,
		IsSymlink: isSymlink,
	}
}

func (i *ImportRequest) Fail(reason string) {
	i.Failed = true
	i.FailedAt = time.Now()
	i.Reason = reason
}

func (i *ImportRequest) Complete() {
	i.Completed = true
	i.CompletedAt = time.Now()
}

func (i *ImportRequest) Process(s *Server) (err error) {
	// Use this for now.
	// This sends the torrent to the arr
	q := s.qbit
	magnet, err := common.GetMagnetFromUrl(i.URI)
	torrent := q.CreateTorrentFromMagnet(magnet, i.Arr.Name)
	debridTorrent, err := debrid.ProcessTorrent(q.Debrid, magnet, i.Arr, i.IsSymlink)
	if err != nil || debridTorrent == nil {
		if err == nil {
			err = fmt.Errorf("failed to process torrent")
		}
		return err
	}
	torrent = q.UpdateTorrentMin(torrent, debridTorrent)
	q.Storage.AddOrUpdate(torrent)
	go q.ProcessFiles(torrent, debridTorrent, i.Arr, i.IsSymlink)
	return nil
}

func (i *ImportRequest) BetaProcess(s *Server) (err error) {
	// THis actually imports the torrent into the arr. Needs more work
	if i.Arr == nil {
		return errors.New("invalid arr")
	}
	q := s.qbit
	magnet, err := common.GetMagnetFromUrl(i.URI)
	if err != nil {
		return fmt.Errorf("error parsing magnet link: %w", err)
	}
	debridTorrent, err := debrid.ProcessTorrent(q.Debrid, magnet, i.Arr, true)
	if err != nil || debridTorrent == nil {
		if err == nil {
			err = errors.New("failed to process torrent")
		}
		return err
	}

	debridTorrent.Arr = i.Arr

	torrentPath, err := q.ProcessSymlink(debridTorrent)
	if err != nil {
		return fmt.Errorf("failed to process symlink: %w", err)
	}
	i.Path = torrentPath
	body, err := i.Arr.Import(torrentPath, i.SeriesId, i.Seasons)
	if err != nil {
		return fmt.Errorf("failed to import: %w", err)
	}
	defer body.Close()

	var resp ManualImportResponseSchema
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	if resp.Status != "success" {
		return fmt.Errorf("failed to import: %s", resp.Result)
	}
	i.Complete()

	return
}

type ImportStore struct {
	Imports map[string]*ImportRequest
	mu      sync.RWMutex
}

func NewImportStore() *ImportStore {
	return &ImportStore{
		Imports: make(map[string]*ImportRequest),
	}
}

func (s *ImportStore) AddImport(i *ImportRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Imports[i.ID] = i
}

func (s *ImportStore) GetImport(id string) *ImportRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Imports[id]
}

func (s *ImportStore) GetAllImports() []*ImportRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var imports []*ImportRequest
	for _, i := range s.Imports {
		imports = append(imports, i)
	}
	return imports
}

func (s *ImportStore) DeleteImport(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Imports, id)
}

func (s *ImportStore) UpdateImport(i *ImportRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Imports[i.ID] = i
}
