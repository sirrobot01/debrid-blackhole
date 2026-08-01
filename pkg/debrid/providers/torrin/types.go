package torrin

import "time"

type envelope[T any] struct {
	Data T `json:"data"`
}

type storeFile struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Link  string `json:"link"`
}

type magnetData struct {
	Id      string      `json:"id"`
	Hash    string      `json:"hash"`
	Magnet  string      `json:"magnet"`
	Name    string      `json:"name"`
	Size    int64       `json:"size"`
	Status  string      `json:"status"`
	Files   []storeFile `json:"files"`
	AddedAt time.Time   `json:"added_at"`
}

type checkItem struct {
	Hash   string      `json:"hash"`
	Name   string      `json:"name"`
	Status string      `json:"status"`
	Files  []storeFile `json:"files"`
}

type checkData struct {
	Items []checkItem `json:"items"`
}

type listData struct {
	Items      []magnetData `json:"items"`
	TotalItems int          `json:"total_items"`
}

type userData struct {
	Id                 string `json:"id"`
	Email              string `json:"email"`
	SubscriptionStatus string `json:"subscription_status"`
}

type linkData struct {
	Link string `json:"link"`
}
