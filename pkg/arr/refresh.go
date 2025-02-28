package arr

import (
	"cmp"
	"fmt"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"net/http"
	"strconv"
	"strings"
)

func (a *Arr) Refresh() error {
	payload := struct {
		Name string `json:"name"`
	}{
		Name: "RefreshMonitoredDownloads",
	}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err == nil && resp != nil {
		statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'
		if statusOk {
			return nil
		}
	}

	return fmt.Errorf("failed to refresh: %v(status: %s)", err, resp.Status)
}

func (a *Arr) Blacklist(infoHash string) error {
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
		url, err := request.JoinURL(a.Host, "history/failed/", strconv.Itoa(torrentId))
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
