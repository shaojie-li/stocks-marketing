package config

import (
	"encoding/base64"
	"errors"
	"os"
)

type Bootstrap struct {
	DatabaseURL string
	MasterKey   []byte
}

func LoadBootstrap() (Bootstrap, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Bootstrap{}, errors.New("DATABASE_URL is required")
	}
	encodedKey := os.Getenv("SETTINGS_MASTER_KEY")
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return Bootstrap{}, errors.New("SETTINGS_MASTER_KEY must be a base64-encoded 32-byte key")
	}
	return Bootstrap{DatabaseURL: databaseURL, MasterKey: key}, nil
}
