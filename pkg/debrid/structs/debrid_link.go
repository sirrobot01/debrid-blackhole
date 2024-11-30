package structs

type DebridLinkAPIResponse[T any] struct {
	Success bool `json:"success"`
	Value   *T   `json:"value"` // Use pointer to allow nil
}

type DebridLinkAvailableResponse DebridLinkAPIResponse[map[string]map[string]struct {
	Name       string `json:"name"`
	HashString string `json:"hashString"`
	Files      []struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	} `json:"files"`
}]

type debridLinkTorrentInfo struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	HashString     string  `json:"hashString"`
	UploadRatio    float64 `json:"uploadRatio"`
	ServerID       string  `json:"serverId"`
	Wait           bool    `json:"wait"`
	PeersConnected int     `json:"peersConnected"`
	Status         int     `json:"status"`
	TotalSize      int64   `json:"totalSize"`
	Files          []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		DownloadURL     string `json:"downloadUrl"`
		Size            int64  `json:"size"`
		DownloadPercent int    `json:"downloadPercent"`
	} `json:"files"`
	Trackers []struct {
		Announce string `json:"announce"`
	} `json:"trackers"`
	Created         int64   `json:"created"`
	DownloadPercent float64 `json:"downloadPercent"`
	DownloadSpeed   int     `json:"downloadSpeed"`
	UploadSpeed     int     `json:"uploadSpeed"`
}

type DebridLinkTorrentInfo DebridLinkAPIResponse[[]debridLinkTorrentInfo]

type DebridLinkSubmitTorrentInfo DebridLinkAPIResponse[debridLinkTorrentInfo]
