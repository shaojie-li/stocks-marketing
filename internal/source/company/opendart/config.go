package opendart

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"
)

const (
	SettingBaseURL        = "fundamental.opendart.base_url"
	SettingAPIKey         = "fundamental.opendart.api_key"
	SettingCorpCode       = "fundamental.opendart.corp_code"
	SettingTargetSymbol   = "fundamental.target_symbol"
	SettingRequestTimeout = "fundamental.opendart.request_timeout"
)

type Settings interface {
	Get(context.Context, string) (string, error)
}
type Config struct {
	BaseURL, APIKey, CorpCode, TargetSymbol string
	RequestTimeout                          time.Duration
}

var digits8 = regexp.MustCompile(`^[0-9]{8}$`)
var key40 = regexp.MustCompile(`^[a-zA-Z0-9]{40}$`)

func LoadConfig(ctx context.Context, settings Settings) (Config, error) {
	values := map[string]string{}
	for _, key := range []string{SettingBaseURL, SettingAPIKey, SettingCorpCode, SettingTargetSymbol, SettingRequestTimeout} {
		value, err := settings.Get(ctx, key)
		if err != nil {
			return Config{}, fmt.Errorf("load %s: %w", key, err)
		}
		values[key] = value
	}
	endpoint, err := url.Parse(values[SettingBaseURL])
	if err != nil || endpoint.Scheme != "https" || endpoint.Host != "opendart.fss.or.kr" || endpoint.Path != "/api" || endpoint.RawQuery != "" || endpoint.User != nil {
		return Config{}, errors.New("fundamental OpenDART base URL must be official")
	}
	if !key40.MatchString(values[SettingAPIKey]) {
		return Config{}, errors.New("fundamental OpenDART API key must contain 40 characters")
	}
	if !digits8.MatchString(values[SettingCorpCode]) || values[SettingCorpCode] != "00164779" {
		return Config{}, errors.New("fundamental OpenDART corp code must be SK hynix")
	}
	if values[SettingTargetSymbol] != "xyz:SKHY" {
		return Config{}, errors.New("fundamental target must be xyz:SKHY")
	}
	timeout, err := time.ParseDuration(values[SettingRequestTimeout])
	if err != nil || timeout <= 0 {
		return Config{}, errors.New("fundamental OpenDART timeout must be positive")
	}
	return Config{values[SettingBaseURL], values[SettingAPIKey], values[SettingCorpCode], values[SettingTargetSymbol], timeout}, nil
}
