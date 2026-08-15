package hyperliquid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	SettingInfoURL        = "market.hyperliquid.info_url"
	SettingWebSocketURL   = "market.hyperliquid.websocket_url"
	SettingAssets         = "market.hyperliquid.assets"
	SettingStaleAfter     = "market.hyperliquid.stale_after"
	SettingRequestTimeout = "market.hyperliquid.request_timeout"
	SettingReconnectMin   = "market.hyperliquid.reconnect_min"
	SettingReconnectMax   = "market.hyperliquid.reconnect_max"
)

type Settings interface {
	Get(context.Context, string) (string, error)
}

type Config struct {
	InfoURL        string
	WebSocketURL   string
	Assets         []string
	StaleAfter     time.Duration
	RequestTimeout time.Duration
	ReconnectMin   time.Duration
	ReconnectMax   time.Duration
}

func LoadConfig(ctx context.Context, settings Settings) (Config, error) {
	values := make(map[string]string, 7)
	for _, key := range []string{SettingInfoURL, SettingWebSocketURL, SettingAssets, SettingStaleAfter, SettingRequestTimeout, SettingReconnectMin, SettingReconnectMax} {
		value, err := settings.Get(ctx, key)
		if err != nil {
			return Config{}, fmt.Errorf("load %s: %w", key, err)
		}
		values[key] = value
	}
	infoURL, err := parseEndpoint(values[SettingInfoURL], "http", "https")
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", SettingInfoURL, err)
	}
	webSocketURL, err := parseEndpoint(values[SettingWebSocketURL], "ws", "wss")
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", SettingWebSocketURL, err)
	}
	var assets []string
	if err := json.Unmarshal([]byte(values[SettingAssets]), &assets); err != nil || len(assets) == 0 {
		return Config{}, errors.New("market.hyperliquid.assets must be a non-empty JSON array")
	}
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		parts := strings.Split(asset, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return Config{}, fmt.Errorf("asset %q is not dex-qualified", asset)
		}
		if _, ok := seen[asset]; ok {
			return Config{}, fmt.Errorf("asset %q is duplicated", asset)
		}
		seen[asset] = struct{}{}
	}
	staleAfter, err := positiveDuration(values[SettingStaleAfter])
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", SettingStaleAfter, err)
	}
	requestTimeout, err := positiveDuration(values[SettingRequestTimeout])
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", SettingRequestTimeout, err)
	}
	reconnectMin, err := positiveDuration(values[SettingReconnectMin])
	if err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", SettingReconnectMin, err)
	}
	reconnectMax, err := positiveDuration(values[SettingReconnectMax])
	if err != nil || reconnectMax < reconnectMin {
		return Config{}, fmt.Errorf("invalid %s: must be a duration not less than reconnect_min", SettingReconnectMax)
	}
	return Config{
		InfoURL: infoURL, WebSocketURL: webSocketURL, Assets: assets,
		StaleAfter: staleAfter, RequestTimeout: requestTimeout,
		ReconnectMin: reconnectMin, ReconnectMax: reconnectMax,
	}, nil
}

func parseEndpoint(raw string, schemes ...string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("endpoint must be an absolute URL without credentials, query or fragment")
	}
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			return parsed.String(), nil
		}
	}
	return "", errors.New("endpoint has unsupported scheme")
}

func positiveDuration(raw string) (time.Duration, error) {
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, errors.New("duration must be positive")
	}
	return duration, nil
}
