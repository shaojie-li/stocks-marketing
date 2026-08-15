package hyperliquid

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/market"
)

type snapshotFunc func(context.Context, []string, uint64, time.Time) ([]market.Snapshot, error)
type streamFunc func(context.Context, string, []string, func(WebSocketMessage) error, func()) error

type Feed struct {
	config   Config
	state    *market.State
	snapshot snapshotFunc
	stream   streamFunc
	now      func() time.Time
	logger   *slog.Logger
}

func NewFeed(config Config, state *market.State, httpClient *http.Client, logger *slog.Logger) *Feed {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	client := NewClient(httpClient, config.InfoURL, config.RequestTimeout)
	feed := newFeed(config, state, client.Snapshot, Stream, time.Now)
	feed.logger = logger
	return feed
}

func newFeed(config Config, state *market.State, snapshot snapshotFunc, stream streamFunc, now func() time.Time) *Feed {
	return &Feed{config: config, state: state, snapshot: snapshot, stream: stream, now: now, logger: slog.New(slog.DiscardHandler)}
}

func (f *Feed) Run(ctx context.Context) error {
	backoff := f.config.ReconnectMin
	for generation := uint64(1); ; generation++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		f.state.BeginRecovery()
		receivedAt := f.now()
		snapshots, err := f.snapshot(ctx, f.config.Assets, generation, receivedAt)
		if err == nil {
			for i := range snapshots {
				snapshots[i].Generation = generation
			}
			if !f.state.ReplaceBatch(snapshots) {
				err = errors.New("market snapshot batch was rejected")
			}
		}
		if err == nil {
			err = f.stream(ctx, f.config.WebSocketURL, f.config.Assets, f.handler(generation), func() {
				f.state.CompleteRecovery(generation)
			})
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			f.logger.Warn("Hyperliquid market cycle failed; gate remains closed", "error", err, "generation", generation, "retry_after", backoff)
		} else {
			f.logger.Warn("Hyperliquid stream ended; gate remains closed", "generation", generation, "retry_after", backoff)
		}
		if err == nil {
			backoff = f.config.ReconnectMin
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > f.config.ReconnectMax {
			backoff = f.config.ReconnectMax
		}
	}
}

func (f *Feed) handler(generation uint64) func(WebSocketMessage) error {
	return func(message WebSocketMessage) error {
		switch message.Kind {
		case MessageBook:
			if message.Book == nil || !f.state.Update(message.Symbol, generation, func(snapshot *market.Snapshot) {
				snapshot.BestBid = message.Book.BestBid.Price
				snapshot.BestAsk = message.Book.BestAsk.Price
				snapshot.ExchangeTime = time.UnixMilli(message.Book.TimeMillis)
				snapshot.ReceivedAt = f.now()
			}) {
				return errors.New("book update did not match current generation")
			}
		case MessageAssetContext:
			if message.Context == nil || !f.state.Update(message.Symbol, generation, func(snapshot *market.Snapshot) {
				snapshot.MarkPrice = message.Context.MarkPrice
				snapshot.OraclePrice = message.Context.OraclePrice
				snapshot.OpenInterest = message.Context.OpenInterest
				snapshot.Funding = message.Context.Funding
				snapshot.ChangePercent = message.Context.ChangePercent
				snapshot.ReceivedAt = f.now()
			}) {
				return errors.New("asset context update did not match current generation")
			}
		}
		return nil
	}
}
