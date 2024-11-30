package arr

import (
	"cmp"
	"fmt"
	"goBlack/common"
	"net/http"
	"strconv"
	"strings"
)

func (a *Arr) Refresh() error {
	payload := map[string]string{"name": "RefreshMonitoredDownloads"}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err == nil && resp != nil {
		statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'
		if statusOk {
			return nil
		}
	}
	return fmt.Errorf("failed to refresh monitored downloads for %s", cmp.Or(a.Name, a.Host))
}

func (a *Arr) MarkAsFailed(infoHash string) error {
	downloadId := strings.ToUpper(infoHash)
	history := a.GetHistory(downloadId, "grabbed")
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
		url, err := common.JoinURL(a.Host, "history/failed/", strconv.Itoa(torrentId))
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
			return fmt.Errorf("failed to mark %s as failed: %v", cmp.Or(a.Name, a.Host), err)
		}
	}
	return nil
}
