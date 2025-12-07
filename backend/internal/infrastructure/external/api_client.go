package external

import (
	"context"
	"net/http"
	"time"
)

// APIClient handles external API communication
type APIClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: baseURL,
	}
}

// FetchData is a sample method for external API calls
func (c *APIClient) FetchData(ctx context.Context, endpoint string) ([]byte, error) {
	// TODO: Implement actual API call
	// Example:
	// req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+endpoint, nil)
	// if err != nil {
	//     return nil, err
	// }
	// resp, err := c.httpClient.Do(req)
	// ...
	return nil, nil
}
