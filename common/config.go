package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

type DebridConfig struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	APIKey           string `json:"api_key"`
	Folder           string `json:"folder"`
	DownloadUncached bool   `json:"download_uncached"`
	CheckCached      bool   `json:"check_cached"`
	RateLimit        string `json:"rate_limit"` // 200/minute or 10/second
}

type ProxyConfig struct {
	Port       string `json:"port"`
	Enabled    bool   `json:"enabled"`
	Debug      bool   `json:"debug"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	CachedOnly *bool  `json:"cached_only"`
}

type QBitTorrentConfig struct {
	Username        string   `json:"username"`
	Password        string   `json:"password"`
	Port            string   `json:"port"`
	Debug           bool     `json:"debug"`
	DownloadFolder  string   `json:"download_folder"`
	Categories      []string `json:"categories"`
	RefreshInterval int      `json:"refresh_interval"`
}

type Config struct {
	Debrid       DebridConfig      `json:"debrid"`
	Debrids      []DebridConfig    `json:"debrids"`
	Proxy        ProxyConfig       `json:"proxy"`
	MaxCacheSize int               `json:"max_cache_size"`
	QBitTorrent  QBitTorrentConfig `json:"qbittorrent"`
}

func validateDebrids(debrids []DebridConfig) error {
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

func validateQbitTorrent(config *QBitTorrentConfig) error {
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

func LoadConfig(path string) (*Config, error) {
	// Load the config file
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(file)

	decoder := json.NewDecoder(file)
	config := &Config{}
	err = decoder.Decode(config)
	if err != nil {
		return nil, err
	}

	if config.Debrid.Name != "" {
		config.Debrids = append(config.Debrids, config.Debrid)
	}

	// Validate the config
	//if err := validateConfig(config); err != nil {
	//	return nil, err
	//}

	return config, nil
}
