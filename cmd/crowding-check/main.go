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
	AsOf     time.Time       `json:"as_of"`
	Crowding domain.Crowding `json:"crowding"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "crowding check failed:", err)
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
	marketConfig, err := hyperliquid.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	client := hyperliquid.NewClient(nil, marketConfig.InfoURL, marketConfig.RequestTimeout)
	asOf := time.Now().UTC()
	result, err := client.SKHYCrowding(ctx, "xyz:SKHY", asOf)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(checkResult{AsOf: asOf, Crowding: result})
}
