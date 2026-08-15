package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shaojie-li/stocks-marketing/internal/app"
	"github.com/shaojie-li/stocks-marketing/internal/config"
	"github.com/shaojie-li/stocks-marketing/internal/market"
	"github.com/shaojie-li/stocks-marketing/internal/market/hyperliquid"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("monitor stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	bootstrap, err := config.LoadBootstrap()
	if err != nil {
		return err
	}
	cipher, err := config.NewCipher(bootstrap.MasterKey)
	if err != nil {
		return err
	}
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	pool, err := pgxpool.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		return errors.New("connect to database")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("ping database")
	}
	for _, table := range []string{"app_settings", "analysis_runs", "delivery_attempts", "river_job"} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil || !exists {
			return fmt.Errorf("required database table %s is missing; run migrations", table)
		}
	}
	application, err := app.New(pool, cipher, nil)
	if err != nil {
		return err
	}
	marketConfig, err := hyperliquid.LoadConfig(ctx, application.Settings())
	if err != nil {
		return fmt.Errorf("load market configuration: %w", err)
	}
	if err := application.Start(ctx); err != nil {
		return fmt.Errorf("start workers: %w", err)
	}
	marketState := market.NewState(marketConfig.Assets, marketConfig.StaleAfter)
	marketFeed := hyperliquid.NewFeed(marketConfig, marketState, nil, logger)
	feedStopped := make(chan error, 1)
	go func() { feedStopped <- marketFeed.Run(ctx) }()
	logger.Info("monitor started")
	select {
	case <-ctx.Done():
	case err := <-feedStopped:
		if err != nil {
			return fmt.Errorf("market feed stopped: %w", err)
		}
		return errors.New("market feed stopped unexpectedly")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := application.Stop(shutdownCtx); err != nil {
		return fmt.Errorf("stop workers: %w", err)
	}
	logger.Info("monitor stopped cleanly")
	return nil
}
