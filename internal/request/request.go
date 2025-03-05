package request

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/time/rate"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func JoinURL(base string, paths ...string) (string, error) {
	// Split the last path component to separate query parameters
	lastPath := paths[len(paths)-1]
	parts := strings.Split(lastPath, "?")
	paths[len(paths)-1] = parts[0]

	joined, err := url.JoinPath(base, paths...)
	if err != nil {
		return "", err
	}

	// Add back query parameters if they exist
	if len(parts) > 1 {
		return joined + "?" + parts[1], nil
	}

	return joined, nil
}

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

func (c *RLHTTPClient) MakeRequest(req *http.Request) ([]byte, error) {
	if c.Headers != nil {
		for key, value := range c.Headers {
			req.Header.Set(key, value)
		}
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(res.Body)

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	statusOk := res.StatusCode >= 200 && res.StatusCode < 300
	if !statusOk {
		// Add status code error to the body
		b = append(b, []byte(fmt.Sprintf("\nstatus code: %d", res.StatusCode))...)
		return nil, errors.New(string(b))
	}

	return b, nil
}

func NewRLHTTPClient(rl *rate.Limiter, headers map[string]string) *RLHTTPClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	c := &RLHTTPClient{
		client: &http.Client{
			Transport: tr,
		},
	}
	if rl != nil {
		c.Ratelimiter = rl
	}
	if headers != nil {
		c.Headers = headers
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

func JSONResponse(w http.ResponseWriter, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		return
	}
}
