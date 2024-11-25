package common

import (
	"encoding/json"
	"log"
	"os"
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

	return config, nil
}
