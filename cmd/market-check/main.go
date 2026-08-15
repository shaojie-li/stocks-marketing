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
	"github.com/shaojie-li/stocks-marketing/internal/market"
	"github.com/shaojie-li/stocks-marketing/internal/market/hyperliquid"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

type result struct {
	Source    string                 `json:"source"`
	CheckedAt time.Time              `json:"checked_at"`
	Freshness string                 `json:"data_freshness"`
	Assets    map[string]assetResult `json:"assets"`
}

type assetResult struct {
	Price         string             `json:"price"`
	ChangePercent string             `json:"change_pct"`
	OraclePrice   string             `json:"oracle_price"`
	OpenInterest  string             `json:"open_interest"`
	BestBid       string             `json:"best_bid"`
	BestAsk       string             `json:"best_ask"`
	ExchangeTime  time.Time          `json:"exchange_timestamp"`
	ReceivedAt    time.Time          `json:"received_at"`
	MarketStatus  string             `json:"market_status"`
	Source        string             `json:"source"`
	Generation    uint64             `json:"generation"`
	Eligibility   market.Eligibility `json:"eligibility"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "market check failed:", err)
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
	state := market.NewState(marketConfig.Assets, marketConfig.StaleAfter)
	feed := hyperliquid.NewFeed(marketConfig, state, nil, nil)
	feedCtx, stopFeed := context.WithCancel(ctx)
	defer stopFeed()
	feedDone := make(chan error, 1)
	go func() { feedDone <- feed.Run(feedCtx) }()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		now := time.Now()
		assets := make(map[string]assetResult, len(marketConfig.Assets))
		allEligible := true
		for _, symbol := range marketConfig.Assets {
			eligibility := state.Eligibility(symbol, now)
			snapshot, _ := state.Snapshot(symbol)
			assets[symbol] = assetResult{
				Price: snapshot.MarkPrice, ChangePercent: snapshot.ChangePercent,
				OraclePrice: snapshot.OraclePrice, OpenInterest: snapshot.OpenInterest,
				BestBid: snapshot.BestBid, BestAsk: snapshot.BestAsk,
				ExchangeTime: snapshot.ExchangeTime.UTC(), ReceivedAt: snapshot.ReceivedAt.UTC(),
				MarketStatus: snapshot.MarketStatus, Source: snapshot.Source,
				Generation: snapshot.Generation, Eligibility: eligibility,
			}
			allEligible = allEligible && eligibility.AnalysisEligible
		}
		if allEligible {
			return json.NewEncoder(os.Stdout).Encode(result{Source: "hyperliquid", CheckedAt: now.UTC(), Freshness: "REALTIME", Assets: assets})
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for all configured assets to pass the data gate")
		case err := <-feedDone:
			if err != nil {
				return err
			}
			return errors.New("market feed stopped before readiness")
		case <-ticker.C:
		}
	}
}
