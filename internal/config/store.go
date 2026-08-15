package config

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

type settingQueries interface {
	GetSetting(context.Context, string) (db.AppSetting, error)
	UpsertSetting(context.Context, db.UpsertSettingParams) (db.AppSetting, error)
}

type Store struct {
	queries settingQueries
	cipher  *Cipher
}

func NewStore(queries settingQueries, cipher *Cipher) *Store {
	return &Store{queries: queries, cipher: cipher}
}

func (s *Store) Set(ctx context.Context, key, value string, secret bool) error {
	if !settingKeyPattern.MatchString(key) {
		return errors.New("invalid setting key")
	}
	storedValue := value
	if secret {
		var err error
		storedValue, err = s.cipher.Encrypt(key, value)
		if err != nil {
			return fmt.Errorf("encrypt setting %s: %w", key, err)
		}
	}
	if _, err := s.queries.UpsertSetting(ctx, db.UpsertSettingParams{
		SettingKey:   key,
		SettingValue: storedValue,
		IsSecret:     secret,
	}); err != nil {
		return fmt.Errorf("store setting %s: %w", key, err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, key string) (string, error) {
	if !settingKeyPattern.MatchString(key) {
		return "", errors.New("invalid setting key")
	}
	setting, err := s.queries.GetSetting(ctx, key)
	if err != nil {
		return "", fmt.Errorf("load setting %s: %w", key, err)
	}
	if !setting.IsSecret {
		return setting.SettingValue, nil
	}
	value, err := s.cipher.Decrypt(key, setting.SettingValue)
	if err != nil {
		return "", fmt.Errorf("decrypt setting %s: %w", key, err)
	}
	return value, nil
}
