package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrRateUnavailable = errors.New("fx rate unavailable for this currency pair")
	ErrRateServiceDown = errors.New("fx service unavailable")
	ErrRateStale       = errors.New("fx rate is too old")
)

// Quote is a rate with the provider and the time it was fetched.
type Quote struct {
	Rate      decimal.Decimal
	Source    string
	FetchedAt time.Time
}

type RateSource interface {
	Rate(ctx context.Context, from, to string) (Quote, error)
}

// Client reads rates from the FX Service.
type Client struct {
	baseURL string
	maxAge  time.Duration
	http    *http.Client
}

func NewClient(baseURL string, maxAge time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		maxAge:  maxAge,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

func (c *Client) Rate(ctx context.Context, from, to string) (Quote, error) {
	u := fmt.Sprintf("%s/rates?%s", c.baseURL, url.Values{"from": {from}, "to": {to}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Quote{}, fmt.Errorf("%w: %v", ErrRateServiceDown, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Quote{}, fmt.Errorf("%w: %v", ErrRateServiceDown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Quote{}, ErrRateUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return Quote{}, fmt.Errorf("%w: status %d", ErrRateServiceDown, resp.StatusCode)
	}

	var body struct {
		Rate      string    `json:"rate"`
		Source    string    `json:"source"`
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Quote{}, fmt.Errorf("%w: %v", ErrRateServiceDown, err)
	}
	rate, err := decimal.NewFromString(body.Rate)
	if err != nil || !rate.IsPositive() {
		return Quote{}, fmt.Errorf("%w: invalid rate %q", ErrRateServiceDown, body.Rate)
	}
	if time.Since(body.FetchedAt) > c.maxAge {
		return Quote{}, ErrRateStale
	}
	return Quote{Rate: rate, Source: body.Source, FetchedAt: body.FetchedAt}, nil
}