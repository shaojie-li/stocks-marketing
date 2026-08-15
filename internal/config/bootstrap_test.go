package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadBootstrapAcceptsOnlyDatabaseAndMasterKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monitor:monitor@localhost/monitor")
	t.Setenv("SETTINGS_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))

	bootstrap, err := LoadBootstrap()
	if err != nil {
		t.Fatalf("LoadBootstrap() error = %v", err)
	}
	if bootstrap.DatabaseURL == "" || len(bootstrap.MasterKey) != 32 {
		t.Fatalf("LoadBootstrap() = %+v", bootstrap)
	}
}

func TestLoadBootstrapDoesNotEchoInvalidSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monitor:monitor@localhost/monitor")
	t.Setenv("SETTINGS_MASTER_KEY", "sensitive-but-invalid")

	_, err := LoadBootstrap()
	if err == nil {
		t.Fatal("LoadBootstrap() accepted an invalid master key")
	}
	if strings.Contains(err.Error(), "sensitive-but-invalid") {
		t.Fatalf("LoadBootstrap() leaked master key: %v", err)
	}
}
