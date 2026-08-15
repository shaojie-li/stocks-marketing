package domain

import (
	"math/big"
	"time"
)

const (
	PriceStructureReasonNotProvided         = "NOT_PROVIDED"
	PriceStructureReasonSourceError         = "SOURCE_ERROR"
	PriceStructureReasonInsufficientHistory = "INSUFFICIENT_HISTORY"
	PriceStructureReasonInvalidHistory      = "INVALID_HISTORY"
)

type DailyPriceBar struct {
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
	Open      string `json:"open"`
	Close     string `json:"close"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Volume    string `json:"volume"`
}

type PriceStructureInput struct {
	Symbol       string
	AsOf         string
	Bars         []DailyPriceBar
	EvidenceRefs []string
}

type PriceStructure struct {
	Availability  string              `json:"availability"`
	Reason        string              `json:"reason,omitempty"`
	Symbol        string              `json:"symbol,omitempty"`
	Interval      string              `json:"interval,omitempty"`
	CompletedBars int                 `json:"completed_bars"`
	WindowStart   string              `json:"window_start,omitempty"`
	WindowEnd     string              `json:"window_end,omitempty"`
	Close         string              `json:"close,omitempty"`
	EMA20         string              `json:"ema20,omitempty"`
	EMA50         string              `json:"ema50,omitempty"`
	ATR14         string              `json:"atr14,omitempty"`
	SupportLow20  string              `json:"support_low_20,omitempty"`
	State         PriceStructureState `json:"state,omitempty"`
	EvidenceRefs  []string            `json:"evidence_refs"`
}

func UnavailablePriceStructure(symbol, reason string, completedBars int, evidenceRefs []string) PriceStructure {
	return PriceStructure{
		Availability: AvailabilityUnavailable, Reason: reason, Symbol: symbol, Interval: "1d",
		CompletedBars: completedBars, EvidenceRefs: append([]string{}, evidenceRefs...),
	}
}

func CalculatePriceStructure(input PriceStructureInput) PriceStructure {
	result := UnavailablePriceStructure(input.Symbol, PriceStructureReasonInvalidHistory, 0, input.EvidenceRefs)
	asOf, err := time.Parse(time.RFC3339Nano, input.AsOf)
	if err != nil || input.Symbol == "" || validateEvidenceRefs(input.EvidenceRefs) != nil {
		return result
	}
	asOf = asOf.UTC()
	completed := make([]DailyPriceBar, 0, len(input.Bars))
	var previousOpen time.Time
	for index, bar := range input.Bars {
		openTime, closeTime, valid := validDailyPriceBar(bar, input.Symbol)
		if !valid || (index > 0 && !openTime.Equal(previousOpen.Add(24*time.Hour))) {
			return result
		}
		previousOpen = openTime
		if closeTime.Before(asOf) {
			completed = append(completed, bar)
		}
	}
	result.CompletedBars = len(completed)
	if len(completed) < 50 {
		result.Reason = PriceStructureReasonInsufficientHistory
		return result
	}

	closes := make([]*big.Rat, len(completed))
	highs := make([]*big.Rat, len(completed))
	lows := make([]*big.Rat, len(completed))
	for index, bar := range completed {
		closes[index], _ = parseDecimal(bar.Close)
		highs[index], _ = parseDecimal(bar.High)
		lows[index], _ = parseDecimal(bar.Low)
	}
	ema20 := exponentialMovingAverage(closes, 20)
	ema50 := exponentialMovingAverage(closes, 50)
	atr14 := averageTrueRange(highs, lows, closes, 14)
	supportLow20 := new(big.Rat).Set(lows[len(lows)-21])
	for _, low := range lows[len(lows)-20 : len(lows)-1] {
		if low.Cmp(supportLow20) < 0 {
			supportLow20.Set(low)
		}
	}
	lastClose := closes[len(closes)-1]
	state := classifyPriceStructure(lastClose, ema20, ema50, atr14, supportLow20)
	result.Availability = AvailabilityAvailable
	result.Reason = ""
	result.WindowStart = completed[0].OpenTime
	result.WindowEnd = completed[len(completed)-1].CloseTime
	result.Close = lastClose.FloatString(8)
	result.EMA20 = ema20.FloatString(8)
	result.EMA50 = ema50.FloatString(8)
	result.ATR14 = atr14.FloatString(8)
	result.SupportLow20 = supportLow20.FloatString(8)
	result.State = state
	return result
}

func classifyPriceStructure(closeValue, ema20, ema50, atr14, supportLow20 *big.Rat) PriceStructureState {
	if closeValue.Cmp(ema20) >= 0 && ema20.Cmp(ema50) >= 0 {
		return PriceStructureAboveSupport
	}
	support := new(big.Rat).Set(ema20)
	if supportLow20.Cmp(support) < 0 {
		support.Set(supportLow20)
	}
	brokenBelow := new(big.Rat).Sub(support, new(big.Rat).Quo(atr14, big.NewRat(4, 1)))
	if closeValue.Cmp(brokenBelow) < 0 {
		return PriceStructureBroken
	}
	return PriceStructureRange
}

func validDailyPriceBar(bar DailyPriceBar, symbol string) (time.Time, time.Time, bool) {
	openTime, openErr := time.Parse(time.RFC3339Nano, bar.OpenTime)
	closeTime, closeErr := time.Parse(time.RFC3339Nano, bar.CloseTime)
	if openErr != nil || closeErr != nil || bar.Symbol != symbol || bar.Interval != "1d" {
		return time.Time{}, time.Time{}, false
	}
	openTime, closeTime = openTime.UTC(), closeTime.UTC()
	if !openTime.Equal(openTime.Truncate(24*time.Hour)) || !closeTime.Equal(openTime.Add(24*time.Hour-time.Millisecond)) {
		return time.Time{}, time.Time{}, false
	}
	values := []string{bar.Open, bar.Close, bar.High, bar.Low, bar.Volume}
	parsed := make([]*big.Rat, len(values))
	for index, value := range values {
		var err error
		parsed[index], err = parseDecimal(value)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
	}
	open, closeValue, high, low, volume := parsed[0], parsed[1], parsed[2], parsed[3], parsed[4]
	if open.Sign() <= 0 || closeValue.Sign() <= 0 || low.Sign() <= 0 || volume.Sign() < 0 || high.Cmp(open) < 0 || high.Cmp(closeValue) < 0 || low.Cmp(open) > 0 || low.Cmp(closeValue) > 0 {
		return time.Time{}, time.Time{}, false
	}
	return openTime, closeTime, true
}

func exponentialMovingAverage(values []*big.Rat, period int) *big.Rat {
	ema := new(big.Rat)
	for _, value := range values[:period] {
		ema.Add(ema, value)
	}
	ema.Quo(ema, big.NewRat(int64(period), 1))
	alpha := big.NewRat(2, int64(period+1))
	for _, value := range values[period:] {
		delta := new(big.Rat).Sub(value, ema)
		ema.Add(ema, delta.Mul(delta, alpha))
	}
	return ema
}

func averageTrueRange(highs, lows, closes []*big.Rat, period int) *big.Rat {
	trueRanges := make([]*big.Rat, len(closes))
	for index := range closes {
		trueRange := new(big.Rat).Sub(highs[index], lows[index])
		if index > 0 {
			highGap := new(big.Rat).Abs(new(big.Rat).Sub(highs[index], closes[index-1]))
			lowGap := new(big.Rat).Abs(new(big.Rat).Sub(lows[index], closes[index-1]))
			if highGap.Cmp(trueRange) > 0 {
				trueRange = highGap
			}
			if lowGap.Cmp(trueRange) > 0 {
				trueRange = lowGap
			}
		}
		trueRanges[index] = trueRange
	}
	atr := new(big.Rat)
	for _, value := range trueRanges[:period] {
		atr.Add(atr, value)
	}
	atr.Quo(atr, big.NewRat(int64(period), 1))
	for _, value := range trueRanges[period:] {
		atr.Mul(atr, big.NewRat(int64(period-1), 1))
		atr.Add(atr, value)
		atr.Quo(atr, big.NewRat(int64(period), 1))
	}
	return atr
}
