package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAnalysisBundleMigrationPreservesLegacyRowsAndRollsBackOnFailure(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	foundation := migrationUp(t, "../../../migrations/001_foundation.sql")
	bundleMigration := migrationUp(t, "../../../migrations/003_analysis_bundle.sql")
	for _, test := range []struct {
		name         string
		forceFailure bool
	}{
		{name: "upgrade"},
		{name: "rollback", forceFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := fmt.Sprintf("t007_%s_%d", test.name, time.Now().UnixNano())
			if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
			if _, err := conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, foundation); err != nil {
				t.Fatalf("apply legacy foundation: %v", err)
			}
			if _, err := conn.Exec(ctx, `
				INSERT INTO analysis_runs (input_hash, rule_version, report, indicators)
				VALUES (decode('01', 'hex'), 'global-analysis/1.1.0', '{"legacy":true}', '{}');
				INSERT INTO delivery_attempts (analysis_run_id, idempotency_key, status)
				VALUES (1, 'legacy:discord', 'PENDING')`); err != nil {
				t.Fatalf("insert legacy rows: %v", err)
			}

			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, migrationErr := tx.Exec(ctx, bundleMigration)
			if migrationErr == nil && test.forceFailure {
				_, migrationErr = tx.Exec(ctx, "SELECT missing_t007_migration_function()")
			}
			if test.forceFailure {
				if migrationErr == nil {
					t.Fatal("injected migration failure did not fail")
				}
				if err := tx.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				var bundleColumnCount int
				if err := conn.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'analysis_runs' AND column_name = 'bundle'`, schema).Scan(&bundleColumnCount); err != nil {
					t.Fatal(err)
				}
				if bundleColumnCount != 0 {
					t.Fatal("failed migration left bundle column behind")
				}
				return
			}
			if migrationErr != nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("apply bundle migration: %v", migrationErr)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var report []byte
			var bundle []byte
			var deliveryRunID int64
			if err := conn.QueryRow(ctx, "SELECT report, bundle FROM analysis_runs WHERE id = 1").Scan(&report, &bundle); err != nil {
				t.Fatal(err)
			}
			if err := conn.QueryRow(ctx, "SELECT analysis_run_id FROM delivery_attempts WHERE idempotency_key = 'legacy:discord'").Scan(&deliveryRunID); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(report), `"legacy": true`) || bundle != nil || deliveryRunID != 1 {
				t.Fatalf("legacy data changed: report=%s bundle=%s delivery_run=%d", report, bundle, deliveryRunID)
			}
		})
	}
}

func migrationUp(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(raw), "---- create above / drop below ----", 2)[0]
}
