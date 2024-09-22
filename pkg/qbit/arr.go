package qbit

import (
	"bytes"
	"cmp"
	"encoding/json"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"net/http"
	gourl "net/url"
	"strconv"
	"strings"
)

func (q *QBit) RefreshArr(arr *debrid.Arr) {
	if arr.Token == "" || arr.Host == "" {
		return
	}
	url, err := common.JoinURL(arr.Host, "api/v3/command")

	if err != nil {
		return
	}
	payload := map[string]string{"name": "RefreshMonitoredDownloads"}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", arr.Token)

	resp, reqErr := client.Do(req)
	if reqErr == nil {
		statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'
		if statusOk {
			if q.debug {
				q.logger.Printf("Refreshed monitored downloads for %s", cmp.Or(arr.Name, arr.Host))
			}
		}
	}
	if reqErr != nil {
	}
}

func (q *QBit) GetArrHistory(arr *debrid.Arr, downloadId, eventType string) *debrid.ArrHistorySchema {
	query := gourl.Values{}
	if downloadId != "" {
		query.Add("downloadId", downloadId)
	}
	query.Add("eventType", eventType)
	query.Add("pageSize", "100")
	url, _ := common.JoinURL(arr.Host, "history")
	url += "?" + query.Encode()
	resp, err := http.Get(url)
	if err != nil {
		return nil
	}
	var data *debrid.ArrHistorySchema

	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	return data

}

func (q *QBit) MarkArrAsFailed(torrent *Torrent, arr *debrid.Arr) error {
	downloadId := strings.ToUpper(torrent.Hash)
	history := q.GetArrHistory(arr, downloadId, "grabbed")
	if history == nil {
		return nil
	}
	torrentId := 0
	for _, record := range history.Records {
		if strings.EqualFold(record.DownloadID, downloadId) {
			torrentId = record.ID
			break
		}
	}
	if torrentId != 0 {
		url, err := common.JoinURL(arr.Host, "history/failed/", strconv.Itoa(torrentId))
		if err != nil {
			return err
		}
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		client := &http.Client{}
		_, err = client.Do(req)
		if err == nil {
			q.logger.Printf("Marked torrent: %s as failed", torrent.Name)
		}
	}
	return nil
}
