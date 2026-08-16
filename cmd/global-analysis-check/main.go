package main

import (
	"context"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "global analysis check failed:", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	dartConfig, err := dart.LoadConfig(ctx, settings)
	if err != nil {
		return err
	}
	marketClient := hyperliquid.NewClient(nil, marketConfig.InfoURL, marketConfig.RequestTimeout)
	observations, err := marketClient.Contract24HObservations(ctx, marketConfig.Assets)
	if err != nil {
		return err
	}
	asOf, err := time.Parse(time.RFC3339Nano, observations[0].WindowEnd)
	if err != nil {
		return errors.New("parse analysis as_of")
	}
	priceStructure, err := marketClient.SKHYPriceStructure(ctx, "xyz:SKHY", asOf)
	if err != nil {
		priceStructure = domain.UnavailablePriceStructure("xyz:SKHY", domain.PriceStructureReasonSourceError, 0, nil)
	}
	selection, err := dart.NewClient(nil, dartConfig).LatestCatalyst(ctx, asOf)
	if err != nil {
		return err
	}
	catalyst := domain.CatalystWithAvailability(selection.Availability, selection.Reason, selection.Event)
	if selection.Availability == domain.AvailabilityAvailable {
		eventAt, parseErr := time.Parse(time.RFC3339Nano, selection.Event.EventAt)
		if parseErr != nil {
			return errors.New("parse DART Catalyst event time")
		}
		window, windowErr := marketClient.CatalystPriceWindow(ctx, eventAt, asOf, dartConfig.TargetSymbol, dartConfig.BenchmarkSymbol)
		if windowErr == nil {
			catalyst, err = domain.EvaluateCatalyst(selection.Event, asOf.Format(time.RFC3339Nano), window)
			if err != nil {
				return err
			}
		} else {
			catalyst = domain.UnavailableCatalyst(domain.CatalystReasonSourceIncomplete, selection.Event)
		}
	}
	bundle, err := domain.BuildAnalysisBundle(domain.AnalysisBundleInput{
		Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano),
		AsOfBucket: asOf.Truncate(time.Minute).Format(time.RFC3339Nano), Observations: observations,
		PriceStructure: priceStructure, Catalyst: catalyst,
	})
	if err != nil {
		return err
	}
	report, err := domain.BuildDegradedShadowReport(bundle)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(report, '\n'))
	return err
}
