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
	"github.com/shaojie-li/stocks-marketing/internal/source/company/opendart"
	"github.com/shaojie-li/stocks-marketing/internal/storage/postgres/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fundamental-check:", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bootstrap, err := config.LoadBootstrap()
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, bootstrap.DatabaseURL)
	if err != nil {
		return errors.New("connect settings database")
	}
	defer pool.Close()
	cipher, err := config.NewCipher(bootstrap.MasterKey)
	if err != nil {
		return err
	}
	settings := config.NewStore(db.New(pool), cipher)
	sourceConfig, err := opendart.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	asOf := time.Now().UTC()
	fundamental, err := opendart.NewClient(nil, sourceConfig).LatestFundamental(ctx, asOf)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"as_of": asOf.Format(time.RFC3339Nano), "fundamental": fundamental})
}
