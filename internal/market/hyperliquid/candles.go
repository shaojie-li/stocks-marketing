package hyperliquid

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

const candleInterval = time.Minute

type Candle struct {
	Symbol    string
	Interval  string
	OpenTime  time.Time
	CloseTime time.Time
	Open      string
	Close     string
	High      string
	Low       string
	Volume    string
	Trades    int
}

type WindowBounds struct {
	TheoreticalStart time.Time
	StartClose       time.Time
	EndObserved      time.Time
	RequestStart     time.Time
	RequestEnd       time.Time
}

func ParseCandles(raw []byte, expectedSymbol, expectedInterval string) ([]Candle, error) {
	var payload []struct {
		OpenTime  int64  `json:"t"`
		CloseTime int64  `json:"T"`
		Symbol    string `json:"s"`
		Interval  string `json:"i"`
		Open      string `json:"o"`
		Close     string `json:"c"`
		High      string `json:"h"`
		Low       string `json:"l"`
		Volume    string `json:"v"`
		Trades    int    `json:"n"`
	}
	if err := decodeOne(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode candleSnapshot: %w", err)
	}
	if len(payload) == 0 {
		return nil, errors.New("candleSnapshot is empty")
	}
	candles := make([]Candle, len(payload))
	for index, item := range payload {
		candles[index] = Candle{
			Symbol: item.Symbol, Interval: item.Interval,
			OpenTime: time.UnixMilli(item.OpenTime).UTC(), CloseTime: time.UnixMilli(item.CloseTime).UTC(),
			Open: item.Open, Close: item.Close, High: item.High, Low: item.Low, Volume: item.Volume, Trades: item.Trades,
		}
	}
	if err := validateCandles(candles, expectedSymbol, expectedInterval); err != nil {
		return nil, err
	}
	return candles, nil
}

func Contract24HBounds(asOf time.Time) WindowBounds {
	endObserved := asOf.UTC()
	theoreticalStart := endObserved.Add(-24 * time.Hour)
	startClose := theoreticalStart.Truncate(candleInterval).Add(-time.Millisecond)
	return WindowBounds{
		TheoreticalStart: theoreticalStart, StartClose: startClose, EndObserved: endObserved,
		RequestStart: startClose.Add(-candleInterval + time.Millisecond),
		RequestEnd:   startClose,
	}
}

func BuildContract24HObservation(candles []Candle, bounds WindowBounds, endPriceRaw string) (domain.Observation, error) {
	if len(candles) != 1 {
		return domain.Observation{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: got %d start-anchor candles, want 1", len(candles))
	}
	if err := validateCandles(candles, candles[0].Symbol, "1m"); err != nil {
		return domain.Observation{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: %w", err)
	}
	if !candles[0].CloseTime.Equal(bounds.StartClose) {
		return domain.Observation{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: candle does not match start anchor")
	}
	anchorLag := bounds.TheoreticalStart.Sub(bounds.StartClose)
	if anchorLag <= 0 || anchorLag >= time.Minute || !bounds.StartClose.Before(bounds.EndObserved) {
		return domain.Observation{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: invalid 24h window")
	}
	startPrice, _ := new(big.Rat).SetString(candles[0].Close)
	if !decimal(endPriceRaw) {
		return domain.Observation{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: end price is not a decimal")
	}
	endPrice, _ := new(big.Rat).SetString(endPriceRaw)
	if startPrice.Sign() <= 0 {
		return domain.Observation{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: start price is not positive")
	}
	if endPrice.Sign() <= 0 {
		return domain.Observation{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: end price is not positive")
	}
	change := new(big.Rat).Sub(endPrice, startPrice)
	change.Quo(change, startPrice)
	change.Mul(change, big.NewRat(100, 1))
	symbol := candles[0].Symbol
	return domain.Observation{
		ID:               fmt.Sprintf("obs:%s:%d:%d", symbol, bounds.StartClose.UnixMilli(), bounds.EndObserved.UnixMilli()),
		Symbol:           symbol,
		Price:            endPriceRaw,
		BaselinePrice:    candles[0].Close,
		BaselineAt:       bounds.StartClose.Format(time.RFC3339Nano),
		ChangePct:        change.FloatString(8),
		ObservedAt:       bounds.EndObserved.Format(time.RFC3339Nano),
		MarketStatus:     "CONTINUOUS",
		WindowType:       "CONTRACT_24H",
		WindowStart:      bounds.StartClose.Format(time.RFC3339Nano),
		WindowEnd:        bounds.EndObserved.Format(time.RFC3339Nano),
		SessionDate:      bounds.EndObserved.Format(time.DateOnly),
		Source:           "hyperliquid",
		SourceTier:       "MARKET_API",
		Freshness:        "REALTIME",
		Adjustment:       "NOT_APPLICABLE",
		TheoreticalStart: bounds.TheoreticalStart.Format(time.RFC3339Nano),
	}, nil
}

func validateCandles(candles []Candle, expectedSymbol, expectedInterval string) error {
	for index, candle := range candles {
		if candle.Symbol != expectedSymbol {
			return fmt.Errorf("candle %d symbol %q does not match %q", index, candle.Symbol, expectedSymbol)
		}
		if candle.Interval != expectedInterval || expectedInterval != "1m" {
			return fmt.Errorf("candle %d interval %q does not match %q", index, candle.Interval, expectedInterval)
		}
		if !candle.CloseTime.Equal(candle.OpenTime.Add(candleInterval - time.Millisecond)) {
			return fmt.Errorf("candle %d has invalid open/close time", index)
		}
		if index > 0 && !candle.OpenTime.Equal(candles[index-1].OpenTime.Add(candleInterval)) {
			return fmt.Errorf("candle %d is duplicate, out of order or separated by a gap", index)
		}
		values := []string{candle.Open, candle.Close, candle.High, candle.Low, candle.Volume}
		for _, value := range values {
			if !decimal(value) {
				return fmt.Errorf("candle %d contains invalid decimal", index)
			}
		}
		open, _ := new(big.Rat).SetString(candle.Open)
		closeValue, _ := new(big.Rat).SetString(candle.Close)
		high, _ := new(big.Rat).SetString(candle.High)
		low, _ := new(big.Rat).SetString(candle.Low)
		volume, _ := new(big.Rat).SetString(candle.Volume)
		if open.Sign() <= 0 || closeValue.Sign() <= 0 || high.Cmp(open) < 0 || high.Cmp(closeValue) < 0 || low.Cmp(open) > 0 || low.Cmp(closeValue) > 0 || low.Sign() <= 0 || volume.Sign() < 0 || candle.Trades < 0 {
			return fmt.Errorf("candle %d has inconsistent OHLCV", index)
		}
	}
	return nil
}
