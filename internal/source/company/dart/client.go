package dart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

const (
	maxRSSResponseBytes = 1 << 20
	maxRequestAttempts  = 3
)

type Client struct {
	httpClient *http.Client
	config     Config
	retryDelay func(int) time.Duration
}

func NewClient(httpClient *http.Client, config Config) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, config: config, retryDelay: fullJitterDelay}
}

func (c *Client) LatestCatalyst(ctx context.Context, asOf time.Time) (Selection, error) {
	raw, err := c.get(ctx)
	if err != nil {
		return Selection{}, err
	}
	feed, err := ParseFeed(raw)
	if err != nil {
		return Selection{}, err
	}
	return SelectCatalyst(feed, asOf, c.config.TargetSymbol), nil
}

func (c *Client) get(ctx context.Context) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		raw, retryable, err := c.getOnce(ctx)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retryable || attempt == maxRequestAttempts-1 {
			return nil, err
		}
		delay := c.retryDelay(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func (c *Client) getOnce(ctx context.Context) ([]byte, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.config.RSSURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create DART RSS request: %w", err)
	}
	request.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9")
	request.Header.Set("User-Agent", "stocks-marketing/1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, fmt.Errorf("call DART RSS: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		if closeErr != nil {
			return nil, true, fmt.Errorf("close DART RSS response: %w", closeErr)
		}
		retryable := retryableHTTPStatus(response.StatusCode)
		return nil, retryable, fmt.Errorf("DART RSS returned HTTP %d", response.StatusCode)
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxRSSResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, true, fmt.Errorf("read DART RSS response: %w", readErr)
	}
	if closeErr != nil {
		return nil, true, fmt.Errorf("close DART RSS response: %w", closeErr)
	}
	if len(raw) > maxRSSResponseBytes {
		return nil, false, errors.New("DART RSS response exceeds 1 MiB")
	}
	return raw, false, nil
}

func fullJitterDelay(attempt int) time.Duration {
	maximum := time.Second * time.Duration(1<<attempt)
	if maximum > 4*time.Second {
		maximum = 4 * time.Second
	}
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
