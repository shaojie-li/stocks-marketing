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
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

type checkResult struct {
	AsOf         time.Time               `json:"as_of"`
	Observations []domain.Observation    `json:"observations"`
	Indicators   domain.CoreIndicatorSet `json:"indicators"`
	Trend        domain.TrendScore       `json:"trend"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "indicator check failed:", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		return errors.New("connect to database")
	}
	defer pool.Close()
	settings := config.NewStore(db.New(pool), cipher)
	marketConfig, err := hyperliquid.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	client := hyperliquid.NewClient(nil, marketConfig.InfoURL, marketConfig.RequestTimeout)
	observations, err := client.Contract24HObservations(ctx, marketConfig.Assets)
	if err != nil {
		return err
	}
	indicators, err := domain.CalculateCoreIndicators(observations)
	if err != nil {
		return err
	}
	trend, err := domain.CalculateTrendScore(domain.TrendScoreInput{Indicators: indicators, Phase: "LIVE_CHECK"})
	if err != nil {
		return err
	}
	asOf, err := time.Parse(time.RFC3339Nano, observations[0].WindowEnd)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(checkResult{AsOf: asOf, Observations: observations, Indicators: indicators, Trend: trend})
}
