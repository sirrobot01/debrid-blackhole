package downloaders

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
)

func GetHTTPClient() *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &http.Client{Transport: tr}
}

func NormalHTTP(client *http.Client, url, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Send the HTTP GET request
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("Error downloading file:", err)
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned non-200 status: %d %s", resp.StatusCode, resp.Status)
	}

	// Write the response body to file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}
	return nil
}
