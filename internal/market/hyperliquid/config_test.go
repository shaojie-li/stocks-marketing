package hyperliquid

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSettings map[string]string

func (f fakeSettings) Get(_ context.Context, key string) (string, error) {
	value, ok := f[key]
	if !ok {
		return "", errors.New("missing setting")
	}
	return value, nil
}

func TestLoadConfigReadsAndValidatesDatabaseSettings(t *testing.T) {
	settings := fakeSettings{
		SettingInfoURL:        "https://api.hyperliquid.xyz/info",
		SettingWebSocketURL:   "wss://api.hyperliquid.xyz/ws",
		SettingAssets:         `["xyz:MU","xyz:SKHY"]`,
		SettingStaleAfter:     "5s",
		SettingRequestTimeout: "15s",
		SettingReconnectMin:   "1s",
		SettingReconnectMax:   "30s",
	}
	config, err := LoadConfig(context.Background(), settings)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.StaleAfter != 5*time.Second || config.RequestTimeout != 15*time.Second {
		t.Fatalf("durations were not loaded exactly: %#v", config)
	}
	if len(config.Assets) != 2 || config.Assets[0] != "xyz:MU" {
		t.Fatalf("assets were not loaded: %#v", config.Assets)
	}
}

func TestLoadConfigRejectsDuplicateOrNonQualifiedAssets(t *testing.T) {
	base := fakeSettings{
		SettingInfoURL: "https://api.hyperliquid.xyz/info", SettingWebSocketURL: "wss://api.hyperliquid.xyz/ws",
		SettingStaleAfter: "5s", SettingRequestTimeout: "15s",
		SettingReconnectMin: "1s", SettingReconnectMax: "30s",
	}
	for name, assets := range map[string]string{
		"duplicate":         `["xyz:MU","xyz:MU"]`,
		"not dex qualified": `["MU"]`,
	} {
		t.Run(name, func(t *testing.T) {
			base[SettingAssets] = assets
			if _, err := LoadConfig(context.Background(), base); err == nil {
				t.Fatal("invalid asset configuration was accepted")
			}
		})
	}
}
