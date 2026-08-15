package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Send(ctx context.Context, webhookURL, idempotencyKey string, report []byte) (string, error) {
	var summary struct {
		RuleVersion string `json:"rule_version"`
		Scores      struct {
			Trend struct {
				Value     float64 `json:"value"`
				Direction string  `json:"direction"`
			} `json:"trend"`
			Entry struct {
				Value     float64 `json:"value"`
				Direction string  `json:"direction"`
			} `json:"entry"`
		} `json:"scores"`
	}
	if err := json.Unmarshal(report, &summary); err != nil {
		return "", errors.New("decode Discord analysis summary")
	}
	payload, err := json.Marshal(map[string]any{
		"content": fmt.Sprintf(
			"Global analysis %s\nTrend %.1f/10 %s\nEntry %.1f/10 %s",
			summary.RuleVersion,
			summary.Scores.Trend.Value,
			summary.Scores.Trend.Direction,
			summary.Scores.Entry.Value,
			summary.Scores.Entry.Direction,
		),
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err != nil {
		return "", errors.New("encode Discord payload")
	}
	parsedURL, err := url.Parse(webhookURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", errors.New("invalid Discord webhook URL")
	}
	query := parsedURL.Query()
	query.Set("wait", "true")
	parsedURL.RawQuery = query.Encode()

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, parsedURL.String(), bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("create Discord request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Idempotency-Key", idempotencyKey)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", errors.New("Discord request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("Discord returned HTTP %d", response.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil || result.ID == "" {
		return "", errors.New("Discord response did not contain a message ID")
	}
	return result.ID, nil
}
