package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

var (
	instance   *Config
	once       sync.Once
	configPath string
)

type Debrid struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	APIKey           string `json:"api_key"`
	Folder           string `json:"folder"`
	DownloadUncached bool   `json:"download_uncached"`
	CheckCached      bool   `json:"check_cached"`
	RateLimit        string `json:"rate_limit"` // 200/minute or 10/second
}

type Proxy struct {
	Port       string `json:"port"`
	Enabled    bool   `json:"enabled"`
	LogLevel   string `json:"log_level"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	CachedOnly *bool  `json:"cached_only"`
}

type QBitTorrent struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	LogLevel        string   `json:"log_level"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	RefreshInterval int      `json:"refresh_interval"`
}

type Arr struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Token string `json:"token"`
}

type Repair struct {
	Enabled      bool   `json:"enabled"`
	Interval     string `json:"interval"`
	RunOnStart   bool   `json:"run_on_start"`
	ZurgURL      string `json:"zurg_url"`
	SkipDeletion bool   `json:"skip_deletion"`
}

type Config struct {
	LogLevel     string      `json:"log_level"`
	Debrid       Debrid      `json:"debrid"`
	Debrids      []Debrid    `json:"debrids"`
	Proxy        Proxy       `json:"proxy"`
	MaxCacheSize int         `json:"max_cache_size"`
	QBitTorrent  QBitTorrent `json:"qbittorrent"`
	Arrs         []Arr       `json:"arrs"`
	Repair       Repair      `json:"repair"`
	AllowedExt   []string    `json:"allowed_file_types"`
	MinFileSize  string      `json:"min_file_size"` // Minimum file size to download, 10MB, 1GB, etc
	MaxFileSize  string      `json:"max_file_size"` // Maximum file size to download (0 means no limit)
}

func (c *Config) loadConfig() error {
	// Load the config file
	if configPath == "" {
		return fmt.Errorf("config path not set")
	}
	file, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(file, &c); err != nil {
		return fmt.Errorf("error unmarshaling config: %w", err)
	}

	if c.Debrid.Name != "" {
		c.Debrids = append(c.Debrids, c.Debrid)
	}

	if len(c.AllowedExt) == 0 {
		c.AllowedExt = getDefaultExtensions()
	}

	// Validate the config
	//if err := validateConfig(c); err != nil {
	//	return nil, err
	//}

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

		// Check folder existence concurrently
		wg.Add(1)
		go func(folder string) {
			defer wg.Done()
			if _, err := os.Stat(folder); os.IsNotExist(err) {
				errChan <- fmt.Errorf("debrid folder does not exist: %s", folder)
			}
		}(debrid.Folder)
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

func validateQbitTorrent(config *QBitTorrent) error {
	if config.DownloadFolder == "" {
		return errors.New("qbittorent download folder is required")
	}
	if _, err := os.Stat(config.DownloadFolder); os.IsNotExist(err) {
		return errors.New("qbittorent download folder does not exist")
	}
	return nil
}

func validateConfig(config *Config) error {
	// Run validations concurrently
	errChan := make(chan error, 2)

	go func() {
		errChan <- validateDebrids(config.Debrids)
	}()

	go func() {
		errChan <- validateQbitTorrent(&config.QBitTorrent)
	}()

	// Check for errors
	for i := 0; i < 2; i++ {
		if err := <-errChan; err != nil {
			return err
		}
	}

	return nil
}

func SetConfigPath(path string) {
	configPath = path
}

func GetConfig() *Config {
	once.Do(func() {
		instance = &Config{} // Initialize instance first
		if err := instance.loadConfig(); err != nil {
			panic(err)
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
