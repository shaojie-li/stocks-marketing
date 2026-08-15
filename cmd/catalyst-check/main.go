package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/market/hyperliquid"
	"github.com/shaojie-li/stocks-marketing/internal/source/company/dart"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

type checkResult struct {
	AsOf     string                    `json:"as_of"`
	Catalyst domain.CatalystEvaluation `json:"catalyst"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "catalyst check failed:", err)
		os.Exit(1)
	}
}

func run() error {
	bootstrap, err := config.LoadBootstrap()
	if err != nil {
		return err
	}
	cipher, err := config.NewCipher(bootstrap.MasterKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		return errors.New("connect to database")
	}
	defer pool.Close()
	settings := config.NewStore(db.New(pool), cipher)
	dartConfig, err := dart.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	marketConfig, err := hyperliquid.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	asOf := time.Now().UTC()
	selection, err := dart.NewClient(nil, dartConfig).LatestCatalyst(ctx, asOf)
	if err != nil {
		return err
	}
	if selection.Availability != domain.AvailabilityAvailable {
		return encodeResult(asOf, domain.CatalystWithAvailability(selection.Availability, selection.Reason, selection.Event))
	}
	eventAt, err := time.Parse(time.RFC3339Nano, selection.Event.EventAt)
	if err != nil {
		return errors.New("parse DART Catalyst event time")
	}
	marketClient := hyperliquid.NewClient(nil, marketConfig.InfoURL, marketConfig.RequestTimeout)
	window, err := marketClient.CatalystPriceWindow(ctx, eventAt, asOf, dartConfig.TargetSymbol, dartConfig.BenchmarkSymbol)
	if err != nil {
		return encodeResult(asOf, domain.UnavailableCatalyst(domain.CatalystReasonSourceIncomplete, selection.Event))
	}
	evaluation, err := domain.EvaluateCatalyst(selection.Event, asOf.Format(time.RFC3339Nano), window)
	if err != nil {
		return err
	}
	return encodeResult(asOf, evaluation)
}

func encodeResult(asOf time.Time, catalyst domain.CatalystEvaluation) error {
	return json.NewEncoder(os.Stdout).Encode(checkResult{
		AsOf: asOf.Format(time.RFC3339Nano), Catalyst: catalyst,
	})
}
