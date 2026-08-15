package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

func TestVerticalSliceIsIdempotentAndKeepsWebhookEncrypted(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
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
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatalf("rivermigrate.New() error = %v", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatalf("River migrate up: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE delivery_attempts, analysis_runs, app_settings, river_job CASCADE"); err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Header.Get("X-Idempotency-Key") == "" {
			t.Error("Discord request is missing idempotency key")
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"id": "message-1"})
	}))
	t.Cleanup(server.Close)

	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("config.NewCipher() error = %v", err)
	}
	application, err := New(pool, cipher, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := application.Settings().Set(ctx, "discord.webhook_url", server.URL, true); err != nil {
		t.Fatalf("store webhook setting: %v", err)
	}
	if err := application.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := application.Stop(stopCtx); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	report, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	start := make(chan struct{})
	results := make(chan Submission, 2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, submitErr := application.Submit(ctx, report)
			results <- result
			errors <- submitErr
		}()
	}
	close(start)
	first, second := <-results, <-results
	if err := <-errors; err != nil {
		t.Fatalf("concurrent Submit() error = %v", err)
	}
	if err := <-errors; err != nil {
		t.Fatalf("concurrent Submit() error = %v", err)
	}
	if first != second {
		t.Fatalf("duplicate Submit() = %+v, want %+v", second, first)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, report); err != nil {
		t.Fatalf("json.Compact() error = %v", err)
	}
	third, err := application.Submit(ctx, compact.Bytes())
	if err != nil {
		t.Fatalf("canonical replay Submit() error = %v", err)
	}
	if third != first {
		t.Fatalf("canonical replay Submit() = %+v, want %+v", third, first)
	}

	queries := db.New(pool)
	for {
		delivery, err := queries.GetDeliveryAttemptByKey(ctx, first.IdempotencyKey)
		if err == nil && delivery.Status == "SUCCEEDED" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for delivery: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}

	runCount, err := queries.CountAnalysisRuns(ctx)
	if err != nil || runCount != 1 {
		t.Fatalf("CountAnalysisRuns() = %d, %v", runCount, err)
	}
	deliveryCount, err := queries.CountDeliveryAttempts(ctx)
	if err != nil || deliveryCount != 1 {
		t.Fatalf("CountDeliveryAttempts() = %d, %v", deliveryCount, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("Discord request count = %d, want 1", requests.Load())
	}

	var storedWebhook string
	if err := pool.QueryRow(ctx, "SELECT setting_value FROM app_settings WHERE setting_key = 'discord.webhook_url'").Scan(&storedWebhook); err != nil {
		t.Fatalf("read stored webhook: %v", err)
	}
	if storedWebhook == server.URL || !bytes.HasPrefix([]byte(storedWebhook), []byte("v1:")) {
		t.Fatalf("webhook was not encrypted at rest: %q", storedWebhook)
	}
}
