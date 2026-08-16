package opendart

import (
	"context"
	"testing"
)

type fakeSettings map[string]string

func (settings fakeSettings) Get(_ context.Context, key string) (string, error) {
	return settings[key], nil
}

func TestLoadConfigRequiresOfficialEndpointAndSecretKey(t *testing.T) {
	config, err := LoadConfig(context.Background(), fakeSettings{SettingBaseURL: "https://opendart.fss.or.kr/api", SettingAPIKey: "1234567890123456789012345678901234567890", SettingCorpCode: "00164779", SettingTargetSymbol: "xyz:SKHY", SettingRequestTimeout: "15s"})
	if err != nil || config.CorpCode != "00164779" || config.TargetSymbol != "xyz:SKHY" {
		t.Fatalf("config=%#v err=%v", config, err)
	}
}
