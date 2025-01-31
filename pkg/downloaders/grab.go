package downloaders

import (
	"crypto/tls"
	"github.com/cavaliergopher/grab/v3"
	"net/http"
	"time"
)

func GetGrabClient() *grab.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyFromEnvironment,
	}
	return &grab.Client{
		UserAgent: "qBitTorrent",
		HTTPClient: &http.Client{
			Transport: tr,
		},
	}
}

func NormalGrab(client *grab.Client, url, filename string, progressCallback func(int64)) error {
	req, err := grab.NewRequest(filename, url)
	if err != nil {
		return err
	}
	resp := client.Do(req)

	t := time.NewTicker(time.Second)
	defer t.Stop()

	var lastReported int64
Loop:
	for {
		select {
		case <-t.C:
			current := resp.BytesComplete()
			if current != lastReported {
				if progressCallback != nil {
					progressCallback(current - lastReported)
				}
				lastReported = current
			}
		case <-resp.Done:
			break Loop
		}
	}

	// Report final bytes
	if progressCallback != nil {
		progressCallback(resp.BytesComplete() - lastReported)
	}

	return resp.Err()
}
