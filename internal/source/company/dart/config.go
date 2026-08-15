package dart

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	SettingRSSURL          = "catalyst.dart.rss_url"
	SettingCorpCode        = "catalyst.dart.corp_code"
	SettingTargetSymbol    = "catalyst.target_symbol"
	SettingBenchmarkSymbol = "catalyst.benchmark_symbol"
	SettingRequestTimeout  = "catalyst.dart.request_timeout"
)

var corpCodePattern = regexp.MustCompile(`^[0-9]{8}$`)

type Settings interface {
	Get(context.Context, string) (string, error)
}

type Config struct {
	RSSURL          string
	CorpCode        string
	TargetSymbol    string
	BenchmarkSymbol string
	RequestTimeout  time.Duration
}

func LoadConfig(ctx context.Context, settings Settings) (Config, error) {
	values := make(map[string]string, 5)
	for _, key := range []string{SettingRSSURL, SettingCorpCode, SettingTargetSymbol, SettingBenchmarkSymbol, SettingRequestTimeout} {
		value, err := settings.Get(ctx, key)
		if err != nil {
			return Config{}, fmt.Errorf("load %s: %w", key, err)
		}
		values[key] = value
	}
	endpoint, err := url.Parse(values[SettingRSSURL])
	if err != nil || endpoint.Scheme != "https" || endpoint.Host != "dart.fss.or.kr" || endpoint.Path != "/api/companyRSS.xml" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return Config{}, errors.New("catalyst.dart.rss_url must be the official DART company RSS endpoint")
	}
	corpCode := values[SettingCorpCode]
	if !corpCodePattern.MatchString(corpCode) {
		return Config{}, errors.New("catalyst.dart.corp_code must contain 8 digits")
	}
	target, benchmark := values[SettingTargetSymbol], values[SettingBenchmarkSymbol]
	if target != "xyz:SKHY" || benchmark != "xyz:SMSN" || !dexQualified(target) || !dexQualified(benchmark) {
		return Config{}, errors.New("Catalyst symbols must be xyz:SKHY and xyz:SMSN")
	}
	timeout, err := time.ParseDuration(values[SettingRequestTimeout])
	if err != nil || timeout <= 0 {
		return Config{}, errors.New("catalyst.dart.request_timeout must be positive")
	}
	query := endpoint.Query()
	query.Set("crpCd", corpCode)
	endpoint.RawQuery = query.Encode()
	return Config{
		RSSURL: endpoint.String(), CorpCode: corpCode, TargetSymbol: target,
		BenchmarkSymbol: benchmark, RequestTimeout: timeout,
	}, nil
}

func dexQualified(symbol string) bool {
	left, right, ok := strings.Cut(symbol, ":")
	return ok && left != "" && right != "" && !strings.Contains(right, ":")
}
