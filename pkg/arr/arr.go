package arr

import (
	"bytes"
	"encoding/json"
	"github.com/sirrobot01/debrid-blackhole/common"
	"net/http"
	"strings"
	"sync"
)

// Type is a type of arr
type Type string

const (
	Sonarr  Type = "sonarr"
	Radarr  Type = "radarr"
	Lidarr  Type = "lidarr"
	Readarr Type = "readarr"
)

var (
	client *common.RLHTTPClient = common.NewRLHTTPClient(nil, nil)
)

type Arr struct {
	Name  string `json:"name"`
	Host  string `json:"host"`
	Token string `json:"token"`
	Type  Type   `json:"type"`
}

func NewArr(name, host, token string, arrType Type) *Arr {
	return &Arr{
		Name:  name,
		Host:  host,
		Token: token,
		Type:  arrType,
	}
}

func (a *Arr) Request(method, endpoint string, payload interface{}) (*http.Response, error) {
	if a.Token == "" || a.Host == "" {
		return nil, nil
	}
	url, err := common.JoinURL(a.Host, endpoint)
	if err != nil {
		return nil, err
	}
	var jsonPayload []byte

	if payload != nil {
		jsonPayload, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", a.Token)
	return client.Do(req)
}

type Storage struct {
	Arrs map[string]*Arr // name -> arr
	mu   sync.RWMutex
}

func inferType(host, name string) Type {
	switch {
	case strings.Contains(host, "sonarr") || strings.Contains(name, "sonarr"):
		return Sonarr
	case strings.Contains(host, "radarr") || strings.Contains(name, "radarr"):
		return Radarr
	case strings.Contains(host, "lidarr") || strings.Contains(name, "lidarr"):
		return Lidarr
	case strings.Contains(host, "readarr") || strings.Contains(name, "readarr"):
		return Readarr
	default:
		return ""
	}
}

func NewStorage(cfg []common.ArrConfig) *Storage {
	arrs := make(map[string]*Arr)
	for _, a := range cfg {
		name := a.Name
		arrs[name] = NewArr(name, a.Host, a.Token, inferType(a.Host, name))
	}
	return &Storage{
		Arrs: arrs,
	}
}

func (as *Storage) AddOrUpdate(arr *Arr) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.Arrs[arr.Name] = arr
}

func (as *Storage) Get(name string) *Arr {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.Arrs[name]
}

func (as *Storage) GetAll() []*Arr {
	as.mu.RLock()
	defer as.mu.RUnlock()
	arrs := make([]*Arr, 0, len(as.Arrs))
	for _, arr := range as.Arrs {
		if arr.Host != "" && arr.Token != "" {
			arrs = append(arrs, arr)
		}
	}
	return arrs
}
