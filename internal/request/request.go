package request

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"golang.org/x/time/rate"
	"io"
	"math"
	"math/rand"
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

type ClientOption func(*Client)

// Client represents an HTTP client with additional capabilities
type Client struct {
	client          *http.Client
	rateLimiter     *rate.Limiter
	headers         map[string]string
	maxRetries      int
	timeout         time.Duration
	skipTLSVerify   bool
	retryableStatus map[int]bool
	logger          zerolog.Logger
}

// WithMaxRetries sets the maximum number of retry attempts
func (c *Client) WithMaxRetries(retries int) *Client {
	c.maxRetries = retries
	return c
}

// WithTimeout sets the request timeout
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	c.timeout = timeout
	return c
}

// WithRateLimiter sets a rate limiter
func (c *Client) WithRateLimiter(rl *rate.Limiter) *Client {
	c.rateLimiter = rl
	return c
}

// WithHeaders sets default headers
func (c *Client) WithHeaders(headers map[string]string) *Client {
	c.headers = headers
	return c
}

func (c *Client) WithLogger(logger zerolog.Logger) *Client {
	c.logger = logger
	return c
}

// WithRetryableStatus adds status codes that should trigger a retry
func (c *Client) WithRetryableStatus(statusCodes ...int) *Client {
	for _, code := range statusCodes {
		c.retryableStatus[code] = true
	}
	return c
}

// doRequest performs a single HTTP request with rate limiting
func (c *Client) doRequest(req *http.Request) (*http.Response, error) {
	if c.rateLimiter != nil {
		err := c.rateLimiter.Wait(req.Context())
		if err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}
	}

	return c.client.Do(req)
}

// Do performs an HTTP request with retries for certain status codes
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	// Save the request body for reuse in retries
	var bodyBytes []byte
	var err error

	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
		req.Body.Close()
	}

	// Apply timeout to the request context if not already present
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx := req.Context()
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	backoff := time.Millisecond * 500
	var resp *http.Response

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Reset the request body if it exists
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Apply headers
		if c.headers != nil {
			for key, value := range c.headers {
				req.Header.Set(key, value)
			}
		}

		resp, err = c.doRequest(req)
		if err != nil {
			// Check if this is a network error that might be worth retrying
			if attempt < c.maxRetries {
				// Apply backoff with jitter
				jitter := time.Duration(rand.Int63n(int64(backoff / 4)))
				sleepTime := backoff + jitter

				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(sleepTime):
					// Continue to next retry attempt
				}

				// Exponential backoff
				backoff *= 2
				continue
			}
			return nil, err
		}

		// Check if the status code is retryable
		if !c.retryableStatus[resp.StatusCode] || attempt == c.maxRetries {
			return resp, nil
		}

		// Close the response body before retrying
		resp.Body.Close()

		// Apply backoff with jitter
		jitter := time.Duration(rand.Int63n(int64(backoff / 4)))
		sleepTime := backoff + jitter

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(sleepTime):
			// Continue to next retry attempt
		}

		// Exponential backoff
		backoff *= 2
	}

	return nil, fmt.Errorf("max retries exceeded")
}

// MakeRequest performs an HTTP request and returns the response body as bytes
func (c *Client) MakeRequest(req *http.Request) ([]byte, error) {
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			c.logger.Printf("Failed to close response body: %v", err)
		}
	}()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP error %d: %s", res.StatusCode, string(bodyBytes))
	}

	return bodyBytes, nil
}

// New creates a new HTTP client with the specified options
func New(options ...ClientOption) *Client {
	client := &Client{
		maxRetries:    3,
		skipTLSVerify: true,
		retryableStatus: map[int]bool{
			http.StatusTooManyRequests:     true,
			http.StatusInternalServerError: true,
			http.StatusBadGateway:          true,
			http.StatusServiceUnavailable:  true,
			http.StatusGatewayTimeout:      true,
		},
		logger: logger.NewLogger("request"),
	}

	// Apply options
	for _, option := range options {
		option(client)
	}

	// Create transport
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: client.skipTLSVerify,
		},
		Proxy: http.ProxyFromEnvironment,
	}

	// Create HTTP client
	client.client = &http.Client{
		Transport: transport,
		Timeout:   client.timeout,
	}

	return client
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
		burstSize := int(math.Max(30, float64(count)*0.25))
		return rate.NewLimiter(rate.Limit(reqsPerSecond), burstSize)
	case "second":
		burstSize := int(math.Max(30, float64(count)*5))
		return rate.NewLimiter(rate.Limit(float64(count)), burstSize)
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
