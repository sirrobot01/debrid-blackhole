package arr

import (
	"encoding/json"
	"net/http"
	gourl "net/url"
)

type HistorySchema struct {
	Page          int    `json:"page"`
	PageSize      int    `json:"pageSize"`
	SortKey       string `json:"sortKey"`
	SortDirection string `json:"sortDirection"`
	TotalRecords  int    `json:"totalRecords"`
	Records       []struct {
		ID         int    `json:"id"`
		DownloadID string `json:"downloadId"`
	} `json:"records"`
}

func (a *Arr) GetHistory(downloadId, eventType string) *HistorySchema {
	query := gourl.Values{}
	if downloadId != "" {
		query.Add("downloadId", downloadId)
	}
	query.Add("eventType", eventType)
	query.Add("pageSize", "100")
	url := "history" + "?" + query.Encode()
	resp, err := a.Request(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var data *HistorySchema

	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	return data

}
