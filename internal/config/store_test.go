package config

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

func TestStorePersistsPlainAndEncryptedSettings(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	lock, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire integration test lock connection: %v", err)
	}
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock(42003)"); err != nil {
		lock.Release()
		t.Fatalf("acquire integration test lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lock.Exec(context.Background(), "SELECT pg_advisory_unlock(42003)")
		lock.Release()
	})
	if _, err := pool.Exec(ctx, "TRUNCATE app_settings"); err != nil {
		t.Fatalf("truncate settings: %v", err)
	}

	cipher, err := NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	store := NewStore(db.New(pool), cipher)

	if err := store.Set(ctx, "analysis.model", "gpt-5.2", false); err != nil {
		t.Fatalf("Set() plain error = %v", err)
	}
	if err := store.Set(ctx, "openai.api_key", "test-database-secret", true); err != nil {
		t.Fatalf("Set() secret error = %v", err)
	}

	plain, err := store.Get(ctx, "analysis.model")
	if err != nil || plain != "gpt-5.2" {
		t.Fatalf("Get() plain = %q, %v", plain, err)
	}
	secret, err := store.Get(ctx, "openai.api_key")
	if err != nil || secret != "test-database-secret" {
		t.Fatalf("Get() secret = %q, %v", secret, err)
	}

	var raw string
	var isSecret bool
	if err := pool.QueryRow(ctx, "SELECT setting_value, is_secret FROM app_settings WHERE setting_key = 'openai.api_key'").Scan(&raw, &isSecret); err != nil {
		t.Fatalf("read raw secret: %v", err)
	}
	if !isSecret || raw == "test-database-secret" || !bytes.HasPrefix([]byte(raw), []byte("v1:")) {
		t.Fatalf("secret was not encrypted at rest: is_secret=%v raw=%q", isSecret, raw)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, value := range []string{"rotated-a", "rotated-b"} {
		go func() {
			<-start
			errors <- store.Set(ctx, "openai.api_key", value, true)
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent Set() error = %v", err)
		}
	}
	var version int64
	if err := pool.QueryRow(ctx, "SELECT version FROM app_settings WHERE setting_key = 'openai.api_key'").Scan(&version); err != nil {
		t.Fatalf("read setting version: %v", err)
	}
	if version != 3 {
		t.Fatalf("concurrent setting version = %d, want 3", version)
	}
	rotated, err := store.Get(ctx, "openai.api_key")
	if err != nil || (rotated != "rotated-a" && rotated != "rotated-b") {
		t.Fatalf("Get() rotated secret returned an unexpected value, err=%v", err)
	}
}

func TestStoreRejectsInvalidSettingKey(t *testing.T) {
	t.Parallel()

	cipher, err := NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	store := NewStore(nil, cipher)
	if err := store.Set(context.Background(), "OpenAI Key", "not-logged", true); err == nil {
		t.Fatal("Set() accepted an invalid setting key")
	}
}
