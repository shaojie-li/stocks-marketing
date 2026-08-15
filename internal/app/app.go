package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/shaojie-li/stocks-marketing/internal/alert/discord"
	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

const discordWebhookSetting = "discord.webhook_url"

type DeliveryArgs struct {
	IdempotencyKey string `json:"idempotency_key" river:"unique"`
}

func (DeliveryArgs) Kind() string { return "discord_delivery" }

func (DeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type deliveryWorker struct {
	river.WorkerDefaults[DeliveryArgs]
	queries  *db.Queries
	settings *config.Store
	discord  *discord.Client
}

func (w *deliveryWorker) Work(ctx context.Context, job *river.Job[DeliveryArgs]) error {
	delivery, err := w.queries.GetDeliveryAttemptByKey(ctx, job.Args.IdempotencyKey)
	if err != nil {
		return fmt.Errorf("load delivery attempt: %w", err)
	}
	if delivery.Status == "SUCCEEDED" {
		return nil
	}
	run, err := w.queries.GetAnalysisRun(ctx, delivery.AnalysisRunID)
	if err != nil {
		return fmt.Errorf("load analysis run: %w", err)
	}
	webhookURL, err := w.settings.Get(ctx, discordWebhookSetting)
	if err != nil {
		return errors.New("load Discord delivery setting")
	}
	messageID, err := w.discord.Send(ctx, webhookURL, delivery.IdempotencyKey, run.Report)
	if err != nil {
		_, _ = w.queries.MarkDeliveryFailed(ctx, db.MarkDeliveryFailedParams{
			ID:        delivery.ID,
			LastError: pgtype.Text{String: "discord delivery failed", Valid: true},
		})
		return err
	}
	if _, err := w.queries.MarkDeliverySucceeded(ctx, db.MarkDeliverySucceededParams{
		ID:                delivery.ID,
		ProviderMessageID: pgtype.Text{String: messageID, Valid: true},
	}); err != nil {
		return fmt.Errorf("mark delivery succeeded: %w", err)
	}
	return nil
}

type App struct {
	pool     *pgxpool.Pool
	queries  *db.Queries
	settings *config.Store
	river    *river.Client[pgx.Tx]
}

type Submission struct {
	AnalysisRunID  int64
	IdempotencyKey string
}

func New(pool *pgxpool.Pool, cipher *config.Cipher, httpClient *http.Client) (*App, error) {
	queries := db.New(pool)
	settings := config.NewStore(queries, cipher)
	workers := river.NewWorkers()
	river.AddWorker(workers, &deliveryWorker{
		queries:  queries,
		settings: settings,
		discord:  discord.NewClient(httpClient),
	})
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create River client: %w", err)
	}
	return &App{pool: pool, queries: queries, settings: settings, river: riverClient}, nil
}

func (a *App) Settings() *config.Store { return a.settings }

func (a *App) Start(ctx context.Context) error { return a.river.Start(ctx) }

func (a *App) Stop(ctx context.Context) error { return a.river.Stop(ctx) }

func (a *App) Submit(ctx context.Context, report []byte) (Submission, error) {
	canonicalReport, err := canonicalJSON(report)
	if err != nil {
		return Submission{}, err
	}
	indicators, err := domain.ComputeIndicators(canonicalReport)
	if err != nil {
		return Submission{}, err
	}
	indicatorJSON, err := json.Marshal(indicators)
	if err != nil {
		return Submission{}, fmt.Errorf("encode indicators: %w", err)
	}
	var metadata struct {
		RuleVersion string `json:"rule_version"`
	}
	if err := json.Unmarshal(canonicalReport, &metadata); err != nil || metadata.RuleVersion == "" {
		return Submission{}, errors.New("analysis report is missing rule_version")
	}
	inputHash := sha256.Sum256(canonicalReport)
	idempotencyKey := hex.EncodeToString(inputHash[:]) + ":discord"

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Submission{}, fmt.Errorf("begin submission: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := a.queries.WithTx(tx)
	run, err := queries.InsertAnalysisRun(ctx, db.InsertAnalysisRunParams{
		InputHash:   inputHash[:],
		RuleVersion: metadata.RuleVersion,
		Report:      canonicalReport,
		Indicators:  indicatorJSON,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		run, err = queries.GetAnalysisRunByInputHash(ctx, inputHash[:])
		if err == nil && (run.RuleVersion != metadata.RuleVersion || !sameIndicators(run.Indicators, indicators)) {
			return Submission{}, errors.New("analysis replay conflicts with stored result")
		}
	}
	if err != nil {
		return Submission{}, fmt.Errorf("store analysis run: %w", err)
	}
	delivery, err := queries.InsertDeliveryAttempt(ctx, db.InsertDeliveryAttemptParams{
		AnalysisRunID:  run.ID,
		IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		delivery, err = queries.GetDeliveryAttemptByKey(ctx, idempotencyKey)
		if err == nil && delivery.AnalysisRunID != run.ID {
			return Submission{}, errors.New("delivery replay conflicts with stored analysis")
		}
	}
	if err != nil {
		return Submission{}, fmt.Errorf("store delivery attempt: %w", err)
	}
	if _, err := a.river.InsertTx(ctx, tx, DeliveryArgs{IdempotencyKey: idempotencyKey}, nil); err != nil {
		return Submission{}, fmt.Errorf("enqueue Discord delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Submission{}, fmt.Errorf("commit submission: %w", err)
	}
	return Submission{AnalysisRunID: run.ID, IdempotencyKey: delivery.IdempotencyKey}, nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("decode analysis report")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("analysis report contains trailing data")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("canonicalize analysis report")
	}
	return canonical, nil
}

func sameIndicators(stored []byte, expected domain.FeatureSnapshot) bool {
	var decoded map[string]string
	return json.Unmarshal(stored, &decoded) == nil && maps.Equal(decoded, expected)
}
