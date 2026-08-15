package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

func TestSubmitAnalysisBundlePersistsComparableScoreAndMemoryHistory(t *testing.T) {
	ctx, pool := integrationPool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
		t.Fatalf("truncate bundle tables: %v", err)
	}
	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(pool, cipher, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}

	input := bundleInput("2026-08-15", "2026-08-14T06:00:00Z", "2026-08-15T06:00:00Z")
	input.PreviousMemory = &domain.MemoryTrendTransition{State: "CALLER_SUPPLIED_STATE", SessionDate: "2026-08-14"}
	first, err := application.SubmitBundle(ctx, input)
	if err != nil {
		t.Fatalf("submit first bundle: %v", err)
	}
	replayed, err := application.SubmitBundle(ctx, input)
	if err != nil || replayed.AnalysisRunID != first.AnalysisRunID || replayed.ScoreID != first.ScoreID || replayed.MemoryHistoryID != first.MemoryHistoryID {
		t.Fatalf("exact replay = %#v, %v; want %#v", replayed, err, first)
	}

	conflict := input
	conflict.Observations = append([]domain.Observation(nil), input.Observations...)
	conflict.Observations[0].ChangePct = "9.99"
	if _, err := application.SubmitBundle(ctx, conflict); err == nil || !strings.Contains(err.Error(), "analysis identity conflicts") {
		t.Fatalf("same identity with changed input error = %v", err)
	}

	secondInput := bundleInput("2026-08-16", "2026-08-15T06:00:00Z", "2026-08-16T06:00:00Z")
	second, err := application.SubmitBundle(ctx, secondInput)
	if err != nil {
		t.Fatalf("submit second bundle: %v", err)
	}
	if second.Bundle.Trend.Direction != domain.ScoreDirectionFlat {
		t.Fatalf("second Trend direction = %s, want FLAT", second.Bundle.Trend.Direction)
	}
	if second.Bundle.Memory.PreviousState != domain.MemoryTrendWeak || second.Bundle.Memory.SessionDate != "2026-08-16" {
		t.Fatalf("second Memory history = %#v", second.Bundle.Memory)
	}

	var runs, scores, memories int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM analysis_runs WHERE bundle IS NOT NULL),
		(SELECT count(*) FROM analysis_scores),
		(SELECT count(*) FROM memory_trend_history)`).Scan(&runs, &scores, &memories); err != nil {
		t.Fatal(err)
	}
	if runs != 2 || scores != 2 || memories != 2 {
		t.Fatalf("history counts = %d/%d/%d, want 2/2/2", runs, scores, memories)
	}
}

func TestSubmitAnalysisBundleReplaysAndConflictsOnPriceStructureInput(t *testing.T) {
	ctx, pool := integrationPool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
		t.Fatal(err)
	}
	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(pool, cipher, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	input := bundleInput("2026-08-15", "2026-08-14T06:00:00Z", "2026-08-15T06:00:00Z")
	input.PriceStructure = domain.PriceStructure{
		Availability: domain.AvailabilityAvailable, Symbol: "xyz:SKHY", Interval: "1d", CompletedBars: 50,
		WindowStart: "2026-06-26T00:00:00Z", WindowEnd: "2026-08-14T23:59:59.999Z",
		Close: "100", EMA20: "99", EMA50: "98", ATR14: "4", SupportLow20: "95",
		State: domain.PriceStructureAboveSupport, EvidenceRefs: []string{"ev-skhy-daily"},
	}
	first, err := application.SubmitBundle(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := application.SubmitBundle(ctx, input)
	if err != nil || replay.AnalysisRunID != first.AnalysisRunID || replay.ScoreID != first.ScoreID || replay.MemoryHistoryID != first.MemoryHistoryID {
		t.Fatalf("Price Structure replay = %#v, %v; want %#v", replay, err, first)
	}
	conflict := input
	conflict.PriceStructure.Close = "101"
	if _, err := application.SubmitBundle(ctx, conflict); err == nil || !strings.Contains(err.Error(), "analysis identity conflicts") {
		t.Fatalf("changed Price Structure conflict error = %v", err)
	}
}

func TestSubmitAnalysisBundleReplaysAndConflictsOnCatalystInput(t *testing.T) {
	ctx, pool := integrationPool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
		t.Fatal(err)
	}
	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(pool, cipher, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	input := bundleInput("2026-08-15", "2026-08-14T08:44:00Z", "2026-08-15T08:44:00Z")
	input.Catalyst = appTestCatalyst(t, "98", "ev-price-v1")
	first, err := application.SubmitBundle(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := application.SubmitBundle(ctx, input)
	if err != nil || replay.AnalysisRunID != first.AnalysisRunID || replay.ScoreID != first.ScoreID || replay.MemoryHistoryID != first.MemoryHistoryID {
		t.Fatalf("Catalyst replay = %#v, %v; want %#v", replay, err, first)
	}
	conflict := input
	conflict.Catalyst = appTestCatalyst(t, "99", "ev-price-v2")
	if _, err := application.SubmitBundle(ctx, conflict); err == nil || !strings.Contains(err.Error(), "analysis identity conflicts") {
		t.Fatalf("changed Catalyst conflict error = %v", err)
	}
}

func appTestCatalyst(t *testing.T, targetEnd, priceEvidence string) domain.CatalystEvaluation {
	t.Helper()
	event := domain.CatalystEvent{
		SourceEventID: "20260814802986", PublishedAt: "2026-08-14T07:44:00Z", EventAt: "2026-08-14T07:44:00Z",
		Source: "dart", SourceTier: "OFFICIAL", OriginalSource: "https://dart.fss.or.kr/api/link.jsp?rcpNo=20260814802986",
		Category: domain.CatalystCategoryDerivativeTradingLoss, AffectedAssets: []string{"xyz:SKHY"},
		Importance: "HIGH", FactStatus: "CONFIRMED", ExpectedDirection: domain.DirectionBearish,
		EvidenceRefs: []string{"ev-dart"},
	}
	result, err := domain.EvaluateCatalyst(event, "2026-08-15T08:44:00Z", domain.CatalystPriceWindow{
		TargetSymbol: "xyz:SKHY", BenchmarkSymbol: "xyz:SMSN",
		WindowStart: "2026-08-14T07:43:59.999Z", WindowEnd: "2026-08-15T07:43:59.999Z",
		TargetStartPrice: "100", TargetEndPrice: targetEnd,
		BenchmarkStartPrice: "100", BenchmarkEndPrice: "99",
		EvidenceRefs: []string{priceEvidence},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSubmitAnalysisBundleSerializesConcurrentReplaysAndRejectsOutOfOrder(t *testing.T) {
	ctx, pool := integrationPool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
		t.Fatal(err)
	}
	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(pool, cipher, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}

	input := bundleInput("2026-08-15", "2026-08-14T06:00:00Z", "2026-08-15T06:00:00Z")
	start := make(chan struct{})
	results := make(chan BundleSubmission, 2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			result, submitErr := application.SubmitBundle(ctx, input)
			results <- result
			errors <- submitErr
		}()
	}
	close(start)
	first, second := <-results, <-results
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if err := <-errors; err != nil {
		t.Fatal(err)
	}
	if first.AnalysisRunID != second.AnalysisRunID || first.ScoreID != second.ScoreID || first.MemoryHistoryID != second.MemoryHistoryID {
		t.Fatalf("concurrent exact replay diverged: %#v / %#v", first, second)
	}

	dayTwo := bundleInput("2026-08-16", "2026-08-15T06:00:00Z", "2026-08-16T06:00:00Z")
	dayThree := bundleInput("2026-08-17", "2026-08-16T06:00:00Z", "2026-08-17T06:00:00Z")
	start = make(chan struct{})
	errors = make(chan error, 2)
	var wait sync.WaitGroup
	for _, next := range []domain.AnalysisBundleInput{dayTwo, dayThree} {
		wait.Add(1)
		go func(candidate domain.AnalysisBundleInput) {
			defer wait.Done()
			<-start
			_, submitErr := application.SubmitBundle(ctx, candidate)
			errors <- submitErr
		}(next)
	}
	close(start)
	wait.Wait()
	close(errors)
	successes := 0
	for submitErr := range errors {
		if submitErr == nil {
			successes++
			continue
		}
		if !strings.Contains(submitErr.Error(), "Memory session is older") {
			t.Fatalf("concurrent adjacent session error = %v", submitErr)
		}
	}
	if successes == 0 {
		t.Fatal("both adjacent sessions failed")
	}

	older := bundleInput("2026-08-14", "2026-08-13T06:00:00Z", "2026-08-14T06:00:00Z")
	if _, err := application.SubmitBundle(ctx, older); err == nil || !strings.Contains(err.Error(), "Memory session is older") {
		t.Fatalf("out-of-order Memory session error = %v", err)
	}
	var runs, scores, memories int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM analysis_runs WHERE bundle IS NOT NULL),
		(SELECT count(*) FROM analysis_scores),
		(SELECT count(*) FROM memory_trend_history)`).Scan(&runs, &scores, &memories); err != nil {
		t.Fatal(err)
	}
	if runs != scores || scores != memories || runs != 1+successes {
		t.Fatalf("concurrent history counts = %d/%d/%d, want %d atomic rows", runs, scores, memories, 1+successes)
	}
}

func TestSubmitAnalysisBundleDetectsConcurrentIdentityConflict(t *testing.T) {
	ctx, pool := integrationPool(t)
	if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
		t.Fatal(err)
	}
	cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	application, err := New(pool, cipher, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	first := bundleInput("2026-08-15", "2026-08-14T06:00:00Z", "2026-08-15T06:00:00Z")
	second := first
	second.Observations = append([]domain.Observation(nil), first.Observations...)
	second.Observations[0].Price = "101"
	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, input := range []domain.AnalysisBundleInput{first, second} {
		go func(candidate domain.AnalysisBundleInput) {
			<-start
			_, submitErr := application.SubmitBundle(ctx, candidate)
			errors <- submitErr
		}(input)
	}
	close(start)
	one, two := <-errors, <-errors
	if (one == nil) == (two == nil) {
		t.Fatalf("concurrent conflict errors = %v / %v, want one success", one, two)
	}
	conflictErr := one
	if conflictErr == nil {
		conflictErr = two
	}
	if !strings.Contains(conflictErr.Error(), "analysis identity conflicts") {
		t.Fatalf("concurrent conflict error = %v", conflictErr)
	}
}

func TestSubmitAnalysisBundleDetectsPersistedFieldConflicts(t *testing.T) {
	tests := map[string]string{
		"run input hash":        `UPDATE analysis_runs SET input_hash = decode(repeat('ab', 32), 'hex') WHERE bundle IS NOT NULL`,
		"run indicators":        `UPDATE analysis_runs SET indicators = '{}' WHERE bundle IS NOT NULL`,
		"run bundle":            `UPDATE analysis_runs SET bundle = '{}' WHERE bundle IS NOT NULL`,
		"score components":      `UPDATE analysis_scores SET component_set = ARRAY['changed']`,
		"score value":           `UPDATE analysis_scores SET value = 0.0`,
		"score direction":       `UPDATE analysis_scores SET direction = 'UP'`,
		"score coverage":        `UPDATE analysis_scores SET coverage_pct = 70`,
		"score confidence":      `UPDATE analysis_scores SET confidence_max = 'LOW'`,
		"score payload":         `UPDATE analysis_scores SET payload = '{}'`,
		"memory previous state": `UPDATE memory_trend_history SET previous_state = 'IMPROVING'`,
		"memory state":          `UPDATE memory_trend_history SET state = 'IMPROVING'`,
		"memory classification": `UPDATE memory_trend_history SET day_classification = 'MARKET_CLOSED'`,
		"memory transitioned":   `UPDATE memory_trend_history SET transitioned = true`,
		"memory supportive":     `UPDATE memory_trend_history SET supportive_streak = 1`,
		"memory adverse":        `UPDATE memory_trend_history SET adverse_streak = 1`,
		"memory reason":         `UPDATE memory_trend_history SET reason = 'changed'`,
		"memory confidence":     `UPDATE memory_trend_history SET confidence_max = 'HIGH'`,
		"memory evidence":       `UPDATE memory_trend_history SET evidence_refs = '["changed"]'`,
		"memory payload":        `UPDATE memory_trend_history SET payload = '{}'`,
	}
	for name, mutation := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, pool := integrationPool(t)
			if _, err := pool.Exec(ctx, "TRUNCATE memory_trend_history, analysis_scores, delivery_attempts, analysis_runs CASCADE"); err != nil {
				t.Fatal(err)
			}
			cipher, err := config.NewCipher(bytes.Repeat([]byte{0x42}, 32))
			if err != nil {
				t.Fatal(err)
			}
			application, err := New(pool, cipher, http.DefaultClient)
			if err != nil {
				t.Fatal(err)
			}
			input := bundleInput("2026-08-15", "2026-08-14T06:00:00Z", "2026-08-15T06:00:00Z")
			if _, err := application.SubmitBundle(ctx, input); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, mutation); err != nil {
				t.Fatalf("mutate persisted field: %v", err)
			}
			if _, err := application.SubmitBundle(ctx, input); err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
				t.Fatalf("persisted field conflict error = %v", err)
			}
		})
	}
}

func integrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func bundleInput(sessionDate, windowStart, windowEnd string) domain.AnalysisBundleInput {
	returns := map[string]string{
		"xyz:MU": "2.40", "xyz:SMH": "1.10", "xyz:XYZ100": "0.40",
		"xyz:AMD": "1.50", "xyz:NVDA": "1.20", "xyz:SKHY": "3.00",
		"xyz:SMSN": "1.00", "xyz:KR200": "0.80",
	}
	observations := make([]domain.Observation, 0, len(returns))
	for symbol, change := range returns {
		observations = append(observations, domain.Observation{
			ID: symbol + ":" + sessionDate, Symbol: symbol, Price: "100", ChangePct: change,
			ObservedAt: windowEnd, MarketStatus: "CONTINUOUS", WindowType: "CONTRACT_24H",
			WindowStart: windowStart, WindowEnd: windowEnd, SessionDate: sessionDate,
			Source: "hyperliquid", SourceTier: "MARKET_API", Freshness: "REALTIME", Adjustment: "NOT_APPLICABLE",
		})
	}
	return domain.AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY", AsOf: windowEnd, AsOfBucket: windowEnd,
		Observations: observations,
	}
}

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
