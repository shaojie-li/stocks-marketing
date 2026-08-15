package dart

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

func TestLoadConfigReadsBoundedDARTSettings(t *testing.T) {
	config, err := LoadConfig(context.Background(), validSettings())
	if err != nil {
		t.Fatal(err)
	}
	if config.RSSURL != "https://dart.fss.or.kr/api/companyRSS.xml?crpCd=00164779" || config.CorpCode != "00164779" || config.TargetSymbol != "xyz:SKHY" || config.BenchmarkSymbol != "xyz:SMSN" || config.RequestTimeout != 15*time.Second {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadConfigRejectsUntrustedOrAmbiguousSettings(t *testing.T) {
	for name, mutate := range map[string]func(fakeSettings){
		"untrusted endpoint": func(settings fakeSettings) { settings[SettingRSSURL] = "https://example.com/api/companyRSS.xml" },
		"invalid corp code":  func(settings fakeSettings) { settings[SettingCorpCode] = "000660" },
		"same symbols":       func(settings fakeSettings) { settings[SettingBenchmarkSymbol] = "xyz:SKHY" },
		"wrong target":       func(settings fakeSettings) { settings[SettingTargetSymbol] = "xyz:MU" },
		"wrong benchmark":    func(settings fakeSettings) { settings[SettingBenchmarkSymbol] = "xyz:KR200" },
		"invalid timeout":    func(settings fakeSettings) { settings[SettingRequestTimeout] = "0s" },
	} {
		t.Run(name, func(t *testing.T) {
			settings := validSettings()
			mutate(settings)
			if _, err := LoadConfig(context.Background(), settings); err == nil {
				t.Fatal("invalid Catalyst config was accepted")
			}
		})
	}
}

func validSettings() fakeSettings {
	return fakeSettings{
		SettingRSSURL: "https://dart.fss.or.kr/api/companyRSS.xml", SettingCorpCode: "00164779",
		SettingTargetSymbol: "xyz:SKHY", SettingBenchmarkSymbol: "xyz:SMSN", SettingRequestTimeout: "15s",
	}
}
