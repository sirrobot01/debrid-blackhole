package config

import (
	"cmp"
	"errors"
	"fmt"
	"github.com/goccy/go-json"
	"os"
	"path/filepath"
	"sync"
)

var (
	instance   *Config
	once       sync.Once
	configPath string
)

type Debrid struct {
	Name             string   `json:"name"`
	Host             string   `json:"host"`
	APIKey           string   `json:"api_key"`
	DownloadAPIKeys  []string `json:"download_api_keys"`
	Folder           string   `json:"folder"`
	DownloadUncached bool     `json:"download_uncached"`
	CheckCached      bool     `json:"check_cached"`
	RateLimit        string   `json:"rate_limit"` // 200/minute or 10/second
	Proxy            string   `json:"proxy"`

	UseWebDav bool `json:"use_webdav"`
	WebDav
}

type QBitTorrent struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	RefreshInterval int      `json:"refresh_interval"`
	SkipPreCache    bool     `json:"skip_pre_cache"`
}

type Arr struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	Token            string `json:"token"`
	Cleanup          bool   `json:"cleanup"`
	SkipRepair       bool   `json:"skip_repair"`
	DownloadUncached *bool  `json:"download_uncached"`
}

type Repair struct {
	Enabled     bool   `json:"enabled"`
	Interval    string `json:"interval"`
	RunOnStart  bool   `json:"run_on_start"`
	ZurgURL     string `json:"zurg_url"`
	AutoProcess bool   `json:"auto_process"`
	UseWebDav   bool   `json:"use_webdav"`
	Workers     int    `json:"workers"`
}

type Auth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type WebDav struct {
	TorrentsRefreshInterval      string `json:"torrents_refresh_interval"`
	DownloadLinksRefreshInterval string `json:"download_links_refresh_interval"`
	Workers                      int    `json:"workers"`
	AutoExpireLinksAfter         string `json:"auto_expire_links_after"`

	// Folder
	FolderNaming string `json:"folder_naming"`

	// Rclone
	RcUrl  string `json:"rc_url"`
	RcUser string `json:"rc_user"`
	RcPass string `json:"rc_pass"`
}

type Config struct {
	LogLevel       string      `json:"log_level"`
	Debrids        []Debrid    `json:"debrids"`
	MaxCacheSize   int         `json:"max_cache_size"`
	QBitTorrent    QBitTorrent `json:"qbittorrent"`
	Arrs           []Arr       `json:"arrs"`
	Repair         Repair      `json:"repair"`
	WebDav         WebDav      `json:"webdav"`
	AllowedExt     []string    `json:"allowed_file_types"`
	MinFileSize    string      `json:"min_file_size"` // Minimum file size to download, 10MB, 1GB, etc
	MaxFileSize    string      `json:"max_file_size"` // Maximum file size to download (0 means no limit)
	Path           string      `json:"-"`             // Path to save the config file
	UseAuth        bool        `json:"use_auth"`
	Auth           *Auth       `json:"-"`
	DiscordWebhook string      `json:"discord_webhook_url"`
}

func (c *Config) JsonFile() string {
	return filepath.Join(c.Path, "config.json")
}
func (c *Config) AuthFile() string {
	return filepath.Join(c.Path, "auth.json")
}

func (c *Config) loadConfig() error {
	// Load the config file
	if configPath == "" {
		return fmt.Errorf("config path not set")
	}
	c.Path = configPath
	file, err := os.ReadFile(c.JsonFile())
	if err != nil {
		return err
	}

	if err := json.Unmarshal(file, &c); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	for i, debrid := range c.Debrids {
		c.Debrids[i] = c.updateDebrid(debrid)
	}

	if len(c.AllowedExt) == 0 {
		c.AllowedExt = getDefaultExtensions()
	}

	// Load the auth file
	c.Auth = c.GetAuth()

	//Validate the config
	if err := validateConfig(c); err != nil {
		return err
	}

	return nil
}

func validateDebrids(debrids []Debrid) error {
	if len(debrids) == 0 {
		return errors.New("no debrids configured")
	}

	errChan := make(chan error, len(debrids))
	var wg sync.WaitGroup

	for _, debrid := range debrids {
		// Basic field validation
		if debrid.Host == "" {
			return errors.New("debrid host is required")
		}
		if debrid.APIKey == "" {
			return errors.New("debrid api key is required")
		}
		if debrid.Folder == "" {
			return errors.New("debrid folder is required")
		}

		// Check folder existence
		//wg.Add(1)
		//go func(folder string) {
		//	defer wg.Done()
		//	if _, err := os.Stat(folder); os.IsNotExist(err) {
		//		errChan <- fmt.Errorf("debrid folder does not exist: %s", folder)
		//	}
		//}(debrid.Folder)
	}

	// Wait for all checks to complete
	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Return first error if any
	if err := <-errChan; err != nil {
		return err
	}

	return nil
}

//func validateQbitTorrent(config *QBitTorrent) error {
//	if config.DownloadFolder == "" {
//		return errors.New("qbittorent download folder is required")
//	}
//	if _, err := os.Stat(config.DownloadFolder); os.IsNotExist(err) {
//		return fmt.Errorf("qbittorent download folder(%s) does not exist", config.DownloadFolder)
//	}
//	return nil
//}

func validateConfig(config *Config) error {
	// Run validations concurrently

	if err := validateDebrids(config.Debrids); err != nil {
		return fmt.Errorf("debrids validation error: %w", err)
	}

	return nil
}

func SetConfigPath(path string) {
	configPath = path
}

func Get() *Config {
	once.Do(func() {
		instance = &Config{} // Initialize instance first
		if err := instance.loadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "configuration Error: %v\n", err)
			os.Exit(1)
		}
	})
	return instance
}

func (c *Config) GetMinFileSize() int64 {
	// 0 means no limit
	if c.MinFileSize == "" {
		return 0
	}
	s, err := parseSize(c.MinFileSize)
	if err != nil {
		return 0
	}
	return s
}

func (c *Config) GetMaxFileSize() int64 {
	// 0 means no limit
	if c.MaxFileSize == "" {
		return 0
	}
	s, err := parseSize(c.MaxFileSize)
	if err != nil {
		return 0
	}
	return s
}

func (c *Config) IsSizeAllowed(size int64) bool {
	if size == 0 {
		return true // Maybe the debrid hasn't reported the size yet
	}
	if c.GetMinFileSize() > 0 && size < c.GetMinFileSize() {
		return false
	}
	if c.GetMaxFileSize() > 0 && size > c.GetMaxFileSize() {
		return false
	}
	return true
}

func (c *Config) GetAuth() *Auth {
	if !c.UseAuth {
		return nil
	}
	if c.Auth == nil {
		c.Auth = &Auth{}
		if _, err := os.Stat(c.AuthFile()); err == nil {
			file, err := os.ReadFile(c.AuthFile())
			if err == nil {
				_ = json.Unmarshal(file, c.Auth)
			}
		}
	}
	return c.Auth
}

func (c *Config) SaveAuth(auth *Auth) error {
	c.Auth = auth
	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}
	return os.WriteFile(c.AuthFile(), data, 0644)
}

func (c *Config) NeedsSetup() bool {
	if c.UseAuth {
		return c.GetAuth().Username == ""
	}
	return false
}

func (c *Config) updateDebrid(d Debrid) Debrid {

	if len(d.DownloadAPIKeys) == 0 {
		d.DownloadAPIKeys = append(d.DownloadAPIKeys, d.APIKey)
	}

	if !d.UseWebDav {
		return d
	}

	if d.TorrentsRefreshInterval == "" {
		d.TorrentsRefreshInterval = cmp.Or(c.WebDav.TorrentsRefreshInterval, "15s") // 15 seconds
	}
	if d.WebDav.DownloadLinksRefreshInterval == "" {
		d.DownloadLinksRefreshInterval = cmp.Or(c.WebDav.DownloadLinksRefreshInterval, "40m") // 40 minutes
	}
	if d.Workers == 0 {
		d.Workers = cmp.Or(c.WebDav.Workers, 30) // 30 workers
	}
	if d.FolderNaming == "" {
		d.FolderNaming = cmp.Or(c.WebDav.FolderNaming, "original_no_ext")
	}
	if d.AutoExpireLinksAfter == "" {
		d.AutoExpireLinksAfter = cmp.Or(c.WebDav.AutoExpireLinksAfter, "3d") // 2 days
	}
	return d
}
