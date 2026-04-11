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

// httpExchange は do() の構造化版。トランスポート失敗・非 2xx・本文を区別して返す。
type httpExchange struct {
	statusCode     int
	body           []byte
	transportError error
}

// DoPrivateRaw は DoPrivate の構造化版。レスポンス本文を非 2xx でも返す。
// 呼び出し側で submitted/failed の判定 (status コード + 本文パース可否) が必要なときに使う。
func (c *RESTClient) DoPrivateRaw(ctx context.Context, method, path, query string, body []byte) (statusCode int, respBody []byte, transportErr error) {
	ex := c.doRaw(ctx, method, path, query, body, true)
	return ex.statusCode, ex.body, ex.transportError
}

func (c *RESTClient) do(ctx context.Context, method, path, query string, body []byte, authenticated bool) ([]byte, error) {
	ex := c.doRaw(ctx, method, path, query, body, authenticated)
	if ex.transportError != nil {
		return nil, ex.transportError
	}
	if ex.statusCode < 200 || ex.statusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", ex.statusCode, string(ex.body))
	}
	return ex.body, nil
}

func (c *RESTClient) doRaw(ctx context.Context, method, path, query string, body []byte, authenticated bool) httpExchange {
	if err := c.waitForRateLimit(ctx); err != nil {
		return httpExchange{transportError: err}
	}

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
		return httpExchange{transportError: fmt.Errorf("failed to create request: %w", err)}
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
		return httpExchange{transportError: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpExchange{
			statusCode:     resp.StatusCode,
			transportError: fmt.Errorf("failed to read response body: %w", err),
		}
	}

	return httpExchange{
		statusCode: resp.StatusCode,
		body:       respBody,
	}
}

// waitForRateLimit enforces the Rakuten API 200ms interval limit.
// Uses a 220ms margin to absorb clock skew between client and Rakuten server,
// since requests pacing exactly at 200ms occasionally trip AUTHENTICATION_ERROR_TOO_MANY_REQUESTS (code 20010).
// Returns an error if the context is cancelled during the wait.
func (c *RESTClient) waitForRateLimit(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastCall)
	if wait := 220*time.Millisecond - elapsed; wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.lastCall = time.Now()
	return nil
}
