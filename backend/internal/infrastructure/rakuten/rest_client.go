package rakuten

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RESTClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string

	mu       sync.Mutex
	lastCall time.Time
}

func NewRESTClient(baseURL, apiKey, apiSecret string) *RESTClient {
	return &RESTClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
	}
}

// DoPublic executes an unauthenticated Public API request.
func (c *RESTClient) DoPublic(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.do(ctx, method, path, query, body, false)
}

// DoPrivate executes an authenticated Private API request.
func (c *RESTClient) DoPrivate(ctx context.Context, method, path, query string, body []byte) ([]byte, error) {
	return c.do(ctx, method, path, query, body, true)
}

func (c *RESTClient) do(ctx context.Context, method, path, query string, body []byte, authenticated bool) ([]byte, error) {
	c.waitForRateLimit()

	url := c.baseURL + path
	if query != "" {
		url += "?" + query
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if authenticated {
		headers := GenerateAuthHeaders(c.apiKey, c.apiSecret, method, path, query, string(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// waitForRateLimit enforces the Rakuten API 200ms interval limit.
func (c *RESTClient) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastCall)
	if elapsed < 200*time.Millisecond {
		time.Sleep(200*time.Millisecond - elapsed)
	}
	c.lastCall = time.Now()
}
