package downloaders

import (
	"crypto/tls"
	"fmt"
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

func NormalGrab(client *grab.Client, url, filename string) error {
	req, err := grab.NewRequest(filename, url)
	if err != nil {
		return err
	}
	resp := client.Do(req)
	if err := resp.Err(); err != nil {
		return err
	}

	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("  %s: transferred %d / %d bytes (%.2f%%)\n",
				resp.Filename,
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}
	if err := resp.Err(); err != nil {
		return err
	}
	return nil
}
