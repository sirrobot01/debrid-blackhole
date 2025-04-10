package request

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/goccy/go-json"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"golang.org/x/net/proxy"
	"golang.org/x/time/rate"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

var (
	once     sync.Once
	instance *Client
)

type ClientOption func(*Client)

// Client represents an HTTP client with additional capabilities
type Client struct {
	client          *http.Client
	rateLimiter     *rate.Limiter
	headers         map[string]string
	headersMu       sync.RWMutex
	maxRetries      int
	timeout         time.Duration
	skipTLSVerify   bool
	retryableStatus map[int]struct{}
	logger          zerolog.Logger
	proxy           string

	// cooldown
	statusCooldowns   map[int]time.Duration
	statusCooldownsMu sync.RWMutex
	lastStatusTime    map[int]time.Time
	lastStatusTimeMu  sync.RWMutex
}

func WithStatusCooldown(statusCode int, cooldown time.Duration) ClientOption {
	return func(c *Client) {
		c.statusCooldownsMu.Lock()
		if c.statusCooldowns == nil {
			c.statusCooldowns = make(map[int]time.Duration)
		}
		c.statusCooldowns[statusCode] = cooldown
		c.statusCooldownsMu.Unlock()
	}
}

// WithMaxRetries sets the maximum number of retry attempts
func WithMaxRetries(maxRetries int) ClientOption {
	return func(c *Client) {
		c.maxRetries = maxRetries
	}
}

// WithTimeout sets the request timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.timeout = timeout
	}
}

func WithRedirectPolicy(policy func(req *http.Request, via []*http.Request) error) ClientOption {
	return func(c *Client) {
		c.client.CheckRedirect = policy
	}
}

// WithRateLimiter sets a rate limiter
func WithRateLimiter(rl *rate.Limiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

// WithHeaders sets default headers
func WithHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		c.headersMu.Lock()
		c.headers = headers
		c.headersMu.Unlock()
	}
}

func (c *Client) SetHeader(key, value string) {
	c.headersMu.Lock()
	c.headers[key] = value
	c.headersMu.Unlock()
}

func WithLogger(logger zerolog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

func WithTransport(transport *http.Transport) ClientOption {
	return func(c *Client) {
		c.client.Transport = transport
	}
}

// WithRetryableStatus adds status codes that should trigger a retry
func WithRetryableStatus(statusCodes ...int) ClientOption {
	return func(c *Client) {
		for _, code := range statusCodes {
			c.retryableStatus[code] = struct{}{}
		}
	}
}

func WithProxy(proxyURL string) ClientOption {
	return func(c *Client) {
		c.proxy = proxyURL
	}
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

	backoff := time.Millisecond * 500
	var resp *http.Response

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Reset the request body if it exists
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// Apply headers
		c.headersMu.RLock()
		if c.headers != nil {
			for key, value := range c.headers {
				req.Header.Set(key, value)
			}
		}
		c.headersMu.RUnlock()

		if attempt > 0 && resp != nil {
			c.statusCooldownsMu.RLock()
			cooldown, exists := c.statusCooldowns[resp.StatusCode]
			c.statusCooldownsMu.RUnlock()

			if exists {
				c.lastStatusTimeMu.RLock()
				lastTime, timeExists := c.lastStatusTime[resp.StatusCode]
				c.lastStatusTimeMu.RUnlock()

				if timeExists {
					elapsed := time.Since(lastTime)
					if elapsed < cooldown {
						// We need to wait longer for this status code
						waitTime := cooldown - elapsed
						select {
						case <-req.Context().Done():
							return nil, req.Context().Err()
						case <-time.After(waitTime):
							// Continue after waiting
						}
					}
				}
			}
		}

		resp, err = c.doRequest(req)

		if err == nil {
			c.lastStatusTimeMu.Lock()
			c.lastStatusTime[resp.StatusCode] = time.Now()
			c.lastStatusTimeMu.Unlock()
		}

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
		if _, ok := c.retryableStatus[resp.StatusCode]; !ok || attempt == c.maxRetries {
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

func (c *Client) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET request: %w", err)
	}

	return c.Do(req)
}

// New creates a new HTTP client with the specified options
func New(options ...ClientOption) *Client {
	client := &Client{
		maxRetries:    3,
		skipTLSVerify: true,
		retryableStatus: map[int]struct{}{
			http.StatusTooManyRequests:     struct{}{},
			http.StatusInternalServerError: struct{}{},
			http.StatusBadGateway:          struct{}{},
			http.StatusServiceUnavailable:  struct{}{},
			http.StatusGatewayTimeout:      struct{}{},
		},
		logger:          logger.New("request"),
		timeout:         60 * time.Second,
		proxy:           "",
		headers:         make(map[string]string), // Initialize headers map
		statusCooldowns: make(map[int]time.Duration),
		lastStatusTime:  make(map[int]time.Time),
	}

	// default http client
	client.client = &http.Client{
		Timeout: client.timeout,
	}

	// Apply options before configuring transport
	for _, option := range options {
		option(client)
	}

	// Check if transport was set by WithTransport option
	if client.client.Transport == nil {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: client.skipTLSVerify,
			},
			// Connection pooling
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			MaxConnsPerHost:     100,

			// Timeouts
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,

			// TCP keep-alive
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,

			// Enable HTTP/2
			ForceAttemptHTTP2: true,

			// Disable compression to save CPU
			DisableCompression: false,
		}

		// Configure proxy if needed
		if client.proxy != "" {
			if strings.HasPrefix(client.proxy, "socks5://") {
				// Handle SOCKS5 proxy
				socksURL, err := url.Parse(client.proxy)
				if err != nil {
					client.logger.Error().Msgf("Failed to parse SOCKS5 proxy URL: %v", err)
				} else {
					auth := &proxy.Auth{}
					if socksURL.User != nil {
						auth.User = socksURL.User.Username()
						password, _ := socksURL.User.Password()
						auth.Password = password
					}

					dialer, err := proxy.SOCKS5("tcp", socksURL.Host, auth, proxy.Direct)
					if err != nil {
						client.logger.Error().Msgf("Failed to create SOCKS5 dialer: %v", err)
					} else {
						transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
							return dialer.Dial(network, addr)
						}
					}
				}
			} else {
				proxyURL, err := url.Parse(client.proxy)
				if err != nil {
					client.logger.Error().Msgf("Failed to parse proxy URL: %v", err)
				} else {
					transport.Proxy = http.ProxyURL(proxyURL)
				}
			}
		} else {
			transport.Proxy = http.ProxyFromEnvironment
		}

		// Set the transport to the client
		client.client.Transport = transport
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

func Gzip(body []byte) []byte {

	var b bytes.Buffer
	if len(body) == 0 {
		return nil
	}
	gz := gzip.NewWriter(&b)
	_, err := gz.Write(body)
	if err != nil {
		return nil
	}
	err = gz.Close()
	if err != nil {
		return nil
	}
	return b.Bytes()
}

func Default() *Client {
	once.Do(func() {
		instance = New()
	})
	return instance
}
