package arr

import (
	"fmt"
	"net/http"
	"strconv"
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

	return fmt.Errorf("failed to refresh: %v", err)
}
