package downloaders

import (
	"crypto/tls"
	"fmt"
	"github.com/valyala/fasthttp"
	"io"
	"os"
)

func GetFastHTTPClient() *fasthttp.Client {
	return &fasthttp.Client{
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

func NormalFastHTTP(client *fasthttp.Client, url, filename string) error {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url)
	req.Header.SetMethod(fasthttp.MethodGet)

	if err := client.Do(req, resp); err != nil {
		return err
	}

	// Check the response status code
	if resp.StatusCode() != fasthttp.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	bodyStream := resp.BodyStream()
	if bodyStream == nil {
		return fmt.Errorf("bodyStream is nil")
	}
	defer func() {
		if rc, ok := bodyStream.(io.Closer); ok {
			rc.Close()
		}
	}()

	if _, err := io.Copy(file, bodyStream); err != nil {
		return err
	}
	return nil
}
