package torrin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/rs/zerolog"
	"go.uber.org/ratelimit"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/internal/request"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

const (
	defaultHost          = "https://api.torrin.app"
	profileCacheDuration = 1 * time.Hour
)

type Torrin struct {
	Host string

	APIKey                string
	accountsManager       *account.Manager
	client                *request.Client
	autoExpiresLinksAfter time.Duration
	logger                zerolog.Logger

	Profile            *types.Profile
	profileLastFetched time.Time
	config             config.Debrid
}

func New(dc config.Debrid, ratelimits map[string]ratelimit.Limiter) (*Torrin, error) {
	headers := map[string]string{"Authorization": "Bearer " + dc.APIKey}
	if dc.UserAgent != "" {
		headers["User-Agent"] = dc.UserAgent
	}
	_log := logger.New(dc.Name)

	expires, err := utils.ParseDuration(dc.AutoExpireLinksAfter)
	if expires == 0 || err != nil {
		expires = 12 * time.Hour
	}

	cfg := config.Get()
	opts := []request.ClientOption{
		request.WithHeaders(headers),
		request.WithMaxRetries(cfg.Retries),
		request.WithRateLimiter(ratelimits["main"]),
		request.WithRetryableStatus(http.StatusTooManyRequests, http.StatusBadGateway),
		request.WithProxy(dc.Proxy),
	}

	c := &Torrin{
		Host:                  defaultHost,
		APIKey:                dc.APIKey,
		accountsManager:       account.NewManager(dc, ratelimits["download"], _log),
		autoExpiresLinksAfter: expires,
		client:                request.New(opts...),
		logger:                _log,
		config:                dc,
	}

	go func() {
		if _, err := c.GetProfile(); err != nil {
			c.logger.Error().Err(err).Msg("Failed to get Torrin profile")
		}
	}()
	return c, nil
}

func (c *Torrin) doGet(endpoint string, result interface{}) (*http.Response, error) {
	return c.doGetWith(c.client, endpoint, result)
}

func (c *Torrin) doGetWith(client *request.Client, endpoint string, result interface{}) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.Host+endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if result != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength != 0 {
		if err := json.ConfigDefault.NewDecoder(resp.Body).Decode(result); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func (c *Torrin) doPostJSON(endpoint string, body interface{}, result interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.Host+endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if result != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.ContentLength != 0 {
		if err := json.ConfigDefault.NewDecoder(resp.Body).Decode(result); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

func mapStatus(s string) types.TorrentStatus {
	switch s {
	case "downloaded", "cached", "complete", "seeding":
		return types.TorrentStatusDownloaded
	case "queued", "pending":
		return types.TorrentStatusQueued
	case "downloading", "processing", "publishing", "uploading":
		return types.TorrentStatusDownloading
	case "failed", "invalid", "error", "evicted":
		return types.TorrentStatusError
	default:
		return types.TorrentStatusDownloading
	}
}

func (c *Torrin) buildFiles(t *types.Torrent, files []storeFile) map[string]types.File {
	out := make(map[string]types.File, len(files))
	cfg := config.Get()
	for _, f := range files {
		if err := cfg.IsFileAllowed(f.Name, f.Size); err != nil {
			continue
		}
		out[f.Name] = types.File{
			TorrentId: t.Id,
			Id:        strconv.Itoa(f.Index),
			Name:      f.Name,
			Path:      f.Name,
			Size:      f.Size,
			Link:      f.Link,
		}
	}
	return out
}

func (c *Torrin) toTorrent(m magnetData) *types.Torrent {
	added := m.AddedAt
	if added.IsZero() {
		added = time.Now()
	}
	t := &types.Torrent{
		Id:               m.Id,
		InfoHash:         m.Hash,
		Name:             m.Name,
		Filename:         m.Name,
		OriginalFilename: m.Name,
		Bytes:            m.Size,
		Size:             m.Size,
		Status:           mapStatus(m.Status),
		Added:            added,
		Debrid:           c.config.Name,
		Files:            map[string]types.File{},
	}
	t.Files = c.buildFiles(t, m.Files)
	return t
}

func (c *Torrin) SubmitMagnet(t *types.Torrent) (*types.Torrent, error) {
	if t.Magnet == nil {
		return nil, fmt.Errorf("torrin: missing magnet")
	}
	var data envelope[magnetData]
	resp, err := c.doPostJSON("/v0/store/magnets", map[string]string{"magnet": t.Magnet.Link}, &data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 429 {
		return nil, customerror.TooManyActiveDownloadsError
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	t.Id = data.Data.Id
	if data.Data.Hash != "" {
		t.InfoHash = data.Data.Hash
	}
	if data.Data.Name != "" {
		t.Name = data.Data.Name
	}
	t.Debrid = c.config.Name
	t.Added = time.Now()
	return t, nil
}

func (c *Torrin) GetTorrent(torrentId string) (*types.Torrent, error) {
	var data envelope[magnetData]
	resp, err := c.doGet("/v0/store/magnets/"+url.PathEscape(torrentId), &data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, customerror.TorrentNotFoundError
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	return c.toTorrent(data.Data), nil
}

func (c *Torrin) UpdateTorrent(t *types.Torrent) error {
	var data envelope[magnetData]
	resp, err := c.doGet("/v0/store/magnets/"+url.PathEscape(t.Id), &data)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return customerror.TorrentNotFoundError
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	m := data.Data
	t.Name = m.Name
	t.Filename = m.Name
	t.OriginalFilename = m.Name
	t.Bytes = m.Size
	t.Status = mapStatus(m.Status)
	if m.Hash != "" {
		t.InfoHash = m.Hash
	}
	t.Debrid = c.config.Name
	t.Files = c.buildFiles(t, m.Files)
	return nil
}

func (c *Torrin) CheckStatus(t *types.Torrent) (*types.Torrent, error) {
	for {
		time.Sleep(2 * time.Second)
		var data envelope[magnetData]
		resp, err := c.doGet("/v0/store/magnets/"+url.PathEscape(t.Id), &data)
		if err != nil {
			return t, err
		}
		if resp.StatusCode != http.StatusOK {
			return t, fmt.Errorf("torrin API error: status %d", resp.StatusCode)
		}
		m := data.Data
		t.Name = m.Name
		t.Filename = m.Name
		t.OriginalFilename = m.Name
		t.Bytes = m.Size
		t.Status = mapStatus(m.Status)
		if m.Hash != "" {
			t.InfoHash = m.Hash
		}
		t.Debrid = c.config.Name

		switch t.Status {
		case types.TorrentStatusDownloaded:
			t.Files = c.buildFiles(t, m.Files)
			if len(t.Files) == 0 {
				return t, fmt.Errorf("no valid files found")
			}
			c.logger.Info().Msgf("Torrent: %s ready on Torrin", t.Name)
			return t, nil
		case types.TorrentStatusError:
			return t, fmt.Errorf("torrent: %s failed on Torrin", t.Name)
		default:
			if !t.DownloadUncached {
				return t, fmt.Errorf("torrent: %s not cached", t.Name)
			}
		}
	}
}

func (c *Torrin) GetTorrents() ([]*types.Torrent, error) {
	var data envelope[listData]
	resp, err := c.doGet("/v0/store/magnets", &data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	out := make([]*types.Torrent, 0, len(data.Data.Items))
	for _, m := range data.Data.Items {
		if mapStatus(m.Status) != types.TorrentStatusDownloaded {
			continue
		}
		out = append(out, c.toTorrent(m))
	}
	return out, nil
}

func (c *Torrin) IsAvailable(hashes []string) map[string]bool {
	result := make(map[string]bool)
	for i := 0; i < len(hashes); i += 100 {
		end := i + 100
		if end > len(hashes) {
			end = len(hashes)
		}
		q := url.Values{}
		for _, h := range hashes[i:end] {
			if h != "" {
				q.Add("magnet", h)
			}
		}
		if len(q) == 0 {
			continue
		}
		var data envelope[checkData]
		resp, err := c.doGet("/v0/store/magnets/check?"+q.Encode(), &data)
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		cached := make(map[string]bool)
		for _, item := range data.Data.Items {
			if item.Status == "cached" || item.Status == "downloaded" {
				cached[strings.ToLower(item.Hash)] = true
			}
		}
		for _, h := range hashes[i:end] {
			if cached[strings.ToLower(h)] {
				result[h] = true
			}
		}
	}
	return result
}

func (c *Torrin) GetDownloadLink(id string, file *types.File) (types.DownloadLink, error) {
	if file.Link == "" {
		return types.DownloadLink{}, fmt.Errorf("torrin: no link for file %s", file.Name)
	}
	final := file.Link
	var data envelope[linkData]
	resp, err := c.doPostJSON("/v0/store/link/generate", map[string]string{"link": file.Link}, &data)
	if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && data.Data.Link != "" {
		final = data.Data.Link
	}
	now := time.Now()
	token := ""
	if cur := c.accountsManager.Current(); cur != nil {
		token = cur.Token
	}
	return types.DownloadLink{
		Debrid:       c.config.Name,
		Token:        token,
		Filename:     file.Name,
		Size:         file.Size,
		Link:         file.Link,
		DownloadLink: final,
		Generated:    now,
		ExpiresAt:    now.Add(c.autoExpiresLinksAfter),
	}, nil
}

func (c *Torrin) DeleteTorrent(torrentId string) error {
	req, err := http.NewRequest(http.MethodDelete, c.Host+"/v0/store/magnets/"+url.PathEscape(torrentId), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Torrin) GetProfile() (*types.Profile, error) {
	if c.Profile != nil && time.Since(c.profileLastFetched) < profileCacheDuration {
		return c.Profile, nil
	}
	var data envelope[userData]
	resp, err := c.doGet("/v0/store/user", &data)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torrin API error: status %d", resp.StatusCode)
	}
	p := &types.Profile{
		Username: data.Data.Email,
		Email:    data.Data.Email,
		Type:     data.Data.SubscriptionStatus,
	}
	c.Profile = p
	c.profileLastFetched = time.Now()
	return p, nil
}

func (c *Torrin) GetAvailableSlots() (int, error) {
	slots := 100 - c.config.MinimumFreeSlot
	if slots < 0 {
		slots = 0
	}
	return slots, nil
}

func (c *Torrin) AccountManager() *account.Manager {
	return c.accountsManager
}

func (c *Torrin) SyncAccounts() {
	c.accountsManager.Sync(c.syncAccount)
}

func (c *Torrin) syncAccount(acc *account.Account) error {
	var data envelope[userData]
	resp, err := c.doGetWith(acc.Client(), "/v0/store/user", &data)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	acc.Username = data.Data.Email
	return nil
}

func (c *Torrin) RefreshDownloadLinks() error {
	return nil
}

func (c *Torrin) CheckFile(ctx context.Context, infohash, link string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return customerror.HosterUnavailableError
	}
	return nil
}

func (c *Torrin) DeleteLink(dl types.DownloadLink) error {
	return nil
}

func (c *Torrin) SpeedTest(ctx context.Context) types.SpeedTestResult {
	result := types.SpeedTestResult{Provider: c.config.Name, TestedAt: time.Now()}
	start := time.Now()
	resp, err := c.doGet("/v0/store/user", nil)
	if err != nil {
		result.Error = fmt.Sprintf("latency test failed: %v", err)
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("latency test unexpected status: %d", resp.StatusCode)
		return result
	}
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

func (c *Torrin) Config() config.Debrid {
	return c.config
}

func (c *Torrin) Logger() zerolog.Logger {
	return c.logger
}

func (c *Torrin) SupportsCheck() bool {
	return true
}
