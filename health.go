package sendafrica

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// HealthStatus is the response from the unauthenticated health check.
type HealthStatus struct {
	Status string `json:"status"`
}

// Health sends a GET to the unversioned /health endpoint. It never requires a
// credential and never retries. A non-200 response returns an *APIError.
func (c *Client) Health(ctx context.Context) (HealthStatus, error) {
	base := strings.TrimRight(c.baseURL, "/")
	endpoint := base
	if idx := strings.LastIndex(endpoint, "/v1"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/health"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthStatus{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HealthStatus{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return HealthStatus{}, &APIError{StatusCode: resp.StatusCode, Body: body, Headers: resp.Header.Clone()}
	}
	var out HealthStatus
	if err := json.Unmarshal(body, &out); err != nil {
		return HealthStatus{}, err
	}
	return out, nil
}

// CountryRate describes one destination on the public international rate card.
type CountryRate struct {
	Name     string `json:"name"`
	ISO2     string `json:"iso2"`
	DialCode string `json:"dial_code"`
	RateTZS  int    `json:"rate_tzs"`
}

// GetRates fetches the full public international rate card. It requires no
// authentication.
func (c *Client) GetRates(ctx context.Context) ([]CountryRate, error) {
	var out []CountryRate
	_, err := c.do(ctx, http.MethodGet, "/rates", nil, nil, &out, RequestOptions{})
	return out, err
}

// GetCountryRate looks up a single country's rate by its ISO2 code or name,
// case-insensitively. Unknown countries return an *APIError.
func (c *Client) GetCountryRate(ctx context.Context, country string) (CountryRate, error) {
	var out CountryRate
	_, err := c.do(ctx, http.MethodGet, "/rates/"+strings.TrimSpace(country), nil, nil, &out, RequestOptions{})
	return out, err
}
