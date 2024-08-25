package common

import (
	"crypto/tls"
	"fmt"
	"golang.org/x/time/rate"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type RLHTTPClient struct {
	client      *http.Client
	Ratelimiter *rate.Limiter
	Headers     map[string]string
}

func (c *RLHTTPClient) Doer(req *http.Request) (*http.Response, error) {
	if c.Ratelimiter != nil {
		err := c.Ratelimiter.Wait(req.Context())
		if err != nil {
			return nil, err
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *RLHTTPClient) Do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	backoff := time.Millisecond * 500

	for i := 0; i < 3; i++ {
		resp, err = c.Doer(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// Close the response body to prevent resource leakage
		resp.Body.Close()

		// Wait for the backoff duration before retrying
		time.Sleep(backoff)

		// Exponential backoff
		backoff *= 2
	}

	return resp, fmt.Errorf("max retries exceeded")
}

func (c *RLHTTPClient) MakeRequest(method string, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if c.Headers != nil {
		for key, value := range c.Headers {
			req.Header.Set(key, value)
		}
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	statusOk := strconv.Itoa(res.StatusCode)[0] == '2'
	if !statusOk {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)
	return io.ReadAll(res.Body)
}

func NewRLHTTPClient(rl *rate.Limiter, headers map[string]string) *RLHTTPClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	c := &RLHTTPClient{
		client: &http.Client{
			Transport: tr,
		},
		Ratelimiter: rl,
		Headers:     headers,
	}
	return c
}

func ParseRateLimit(rateStr string) *rate.Limiter {
	if rateStr == "" {
		return nil
	}
	re := regexp.MustCompile(`(\d+)/(minute|second)`)
	matches := re.FindStringSubmatch(rateStr)
	if len(matches) != 3 {
		return nil
	}

	count, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil
	}

	unit := matches[2]
	switch unit {
	case "minute":
		reqsPerSecond := float64(count) / 60.0
		return rate.NewLimiter(rate.Limit(reqsPerSecond), 5)
	case "second":
		return rate.NewLimiter(rate.Limit(float64(count)), 5)
	default:
		return nil
	}
}
