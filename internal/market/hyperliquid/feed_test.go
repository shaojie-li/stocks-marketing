package hyperliquid

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/market"
)

func TestFeedReconnectsWithGateClosedUntilFreshSnapshotAndAcknowledgements(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := market.NewState([]string{"xyz:SKHY"}, 5*time.Second)
	config := Config{Assets: []string{"xyz:SKHY"}, StaleAfter: 5 * time.Second, ReconnectMin: time.Millisecond, ReconnectMax: 2 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var streamCalls atomic.Int32
	feed := newFeed(config, state,
		func(context.Context, []string, uint64, time.Time) ([]market.Snapshot, error) {
			return []market.Snapshot{{
				Symbol: "xyz:SKHY", MarkPrice: "166.18", OraclePrice: "166.22", OpenInterest: "1",
				BestBid: "166.18", BestAsk: "166.19", ExchangeTime: now, ReceivedAt: now,
			}}, nil
		},
		func(_ context.Context, _ string, _ []string, _ func(WebSocketMessage) error, ready func()) error {
			call := streamCalls.Add(1)
			if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible {
				t.Fatalf("gate opened before subscription acknowledgements on call %d", call)
			}
			if call == 1 {
				return errors.New("simulated disconnect")
			}
			ready()
			if got := state.Eligibility("xyz:SKHY", now); !got.AnalysisEligible {
				t.Fatalf("gate did not open after resync and acknowledgements: %#v", got)
			}
			cancel()
			return nil
		},
		func() time.Time { return now },
	)
	if err := feed.Run(ctx); err != nil {
		t.Fatalf("run feed: %v", err)
	}
	if streamCalls.Load() != 2 {
		t.Fatalf("stream calls = %d, want 2", streamCalls.Load())
	}
}
