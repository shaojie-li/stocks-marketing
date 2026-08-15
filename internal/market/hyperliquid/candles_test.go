package hyperliquid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCandlesPreservesFirstPartyDecimalStrings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "data-sources", "hyperliquid", "t005-candles-1m.json"))
	if err != nil {
		t.Fatal(err)
	}
	candles, err := ParseCandles(raw, "xyz:MU", "1m")
	if err != nil {
		t.Fatalf("parse candles: %v", err)
	}
	if len(candles) != 5 || candles[0].Open != "973.7" || candles[0].Volume != "6.088" || candles[4].Close != "973.58" {
		t.Fatalf("candle strings were changed: %#v", candles)
	}
}

func TestParseCandlesAcceptsContinuousDailyHistory(t *testing.T) {
	raw := []byte(`[
		{"t":1783468800000,"T":1783555199999,"s":"xyz:SKHY","i":"1d","o":"100","c":"101","h":"102","l":"99","v":"10","n":1},
		{"t":1783555200000,"T":1783641599999,"s":"xyz:SKHY","i":"1d","o":"101","c":"102","h":"103","l":"100","v":"11","n":2}
	]`)
	candles, err := ParseCandles(raw, "xyz:SKHY", "1d")
	if err != nil {
		t.Fatalf("parse daily candles: %v", err)
	}
	if len(candles) != 2 || candles[1].Close != "102" || candles[1].Interval != "1d" {
		t.Fatalf("daily candles = %#v", candles)
	}
}

func TestParseCandlesRejectsProtocolAndContinuityErrors(t *testing.T) {
	valid := `[
		{"t":60000,"T":119999,"s":"xyz:MU","i":"1m","o":"100","c":"101","h":"102","l":"99","v":"1.1","n":3},
		{"t":120000,"T":179999,"s":"xyz:MU","i":"1m","o":"101","c":"102","h":"103","l":"100","v":"1.2","n":4}
	]`
	tests := map[string]string{
		"wrong symbol":      strings.Replace(valid, "xyz:MU", "xyz:SMH", 1),
		"wrong interval":    strings.Replace(valid, `"1m"`, `"5m"`, 1),
		"gap":               strings.Replace(valid, `"t":120000`, `"t":180000`, 1),
		"overlapping close": strings.Replace(valid, `"T":119999`, `"T":179999`, 1),
		"fraction":          strings.Replace(valid, `"c":"101"`, `"c":"1/2"`, 1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCandles([]byte(payload), "xyz:MU", "1m"); err == nil {
				t.Fatal("invalid candle payload was accepted")
			}
		})
	}
}

func TestContract24HBoundsUseCompletedStartCandleAndCurrentBatchEnd(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 10, 25, 30, 123000000, time.UTC)
	bounds := Contract24HBounds(asOf)
	if !bounds.EndObserved.Equal(asOf) {
		t.Fatalf("end observed = %s, want %s", bounds.EndObserved, asOf)
	}
	if got, want := bounds.TheoreticalStart, asOf.Add(-24*time.Hour); !got.Equal(want) {
		t.Fatalf("theoretical start = %s, want %s", got, want)
	}
	if bounds.TheoreticalStart.Sub(bounds.StartClose) <= 0 || bounds.TheoreticalStart.Sub(bounds.StartClose) >= time.Minute {
		t.Fatalf("start anchor is not the nearest completed minute: %#v", bounds)
	}
	if !bounds.RequestStart.Equal(bounds.StartClose.Add(-time.Minute + time.Millisecond)) {
		t.Fatalf("request start does not include the start anchor candle: %#v", bounds)
	}
	if !bounds.RequestEnd.Equal(bounds.StartClose) {
		t.Fatalf("request should contain only the start anchor: %#v", bounds)
	}
}

func TestBuildContract24HObservationComputesExactReturnAndRejectsGap(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 10, 25, 30, 0, time.UTC)
	bounds := Contract24HBounds(asOf)
	candles := []Candle{{Symbol: "xyz:MU", Interval: "1m", OpenTime: bounds.RequestStart, CloseTime: bounds.StartClose, Open: "100", Close: "100", High: "101", Low: "99", Volume: "1", Trades: 1}}
	observation, err := BuildContract24HObservation(candles, bounds, "101.00000001")
	if err != nil {
		t.Fatalf("build observation: %v", err)
	}
	if observation.ChangePct != "1.00000001" || observation.Price != "101.00000001" {
		t.Fatalf("return lost precision: %#v", observation)
	}
	if observation.BaselinePrice != "100" || observation.BaselineAt != bounds.StartClose.Format(time.RFC3339Nano) {
		t.Fatalf("baseline audit fields are incomplete: %#v", observation)
	}
	if observation.WindowStart != bounds.StartClose.Format(time.RFC3339Nano) || observation.WindowEnd != bounds.EndObserved.Format(time.RFC3339Nano) {
		t.Fatalf("effective window was not audited: %#v", observation)
	}
	if observation.TheoreticalStart != bounds.TheoreticalStart.Format(time.RFC3339Nano) {
		t.Fatalf("theoretical start was not audited: %#v", observation)
	}
	candles[0].CloseTime = bounds.StartClose.Add(time.Minute)
	if _, err := BuildContract24HObservation(candles, bounds, "101"); err == nil {
		t.Fatal("candle not matching the start anchor was accepted")
	}
}
