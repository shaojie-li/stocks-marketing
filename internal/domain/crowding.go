package domain

import (
	"errors"
	"math/big"
	"slices"
	"time"
)

const (
	AvailabilityStale = "STALE"

	CrowdingRuleVersion = "crowding/1.0.0"

	CrowdingInputFunding        = "funding"
	CrowdingInputOpenInterest   = "open_interest"
	CrowdingInputPremium        = "premium"
	CrowdingInputPriceExtension = "price_extension"
	CrowdingInputVolume         = "volume"

	CrowdingReasonInsufficientInputs      = "INSUFFICIENT_INPUTS"
	CrowdingReasonInsufficientHistory     = "INSUFFICIENT_HISTORY"
	CrowdingReasonInvalidHistory          = "INVALID_HISTORY"
	CrowdingReasonHistoricalOIUnavailable = "HISTORICAL_OI_UNAVAILABLE"
	CrowdingReasonNotProvided             = "NOT_PROVIDED"
	CrowdingReasonSourceError             = "SOURCE_ERROR"
	CrowdingReasonStale                   = "STALE"

	CrowdingLow     CrowdingState = "LOW"
	CrowdingNormal  CrowdingState = "NORMAL"
	CrowdingHigh    CrowdingState = "HIGH"
	CrowdingExtreme CrowdingState = "EXTREME"

	CrowdingLong  CrowdingDirection = "LONG"
	CrowdingShort CrowdingDirection = "SHORT"
	CrowdingMixed CrowdingDirection = "MIXED"
)

type CrowdingState string
type CrowdingDirection string

type CrowdingFundingSample struct {
	Symbol      string `json:"symbol"`
	Time        string `json:"time"`
	FundingRate string `json:"funding_rate"`
	Premium     string `json:"premium"`
}

type CrowdingComponent struct {
	Name         string            `json:"name"`
	Availability string            `json:"availability"`
	Reason       string            `json:"reason,omitempty"`
	WindowStart  string            `json:"window_start,omitempty"`
	WindowEnd    string            `json:"window_end,omitempty"`
	CurrentValue string            `json:"current_value,omitempty"`
	Percentile   string            `json:"percentile,omitempty"`
	Triggered    bool              `json:"triggered"`
	Direction    CrowdingDirection `json:"direction,omitempty"`
	EvidenceRefs []string          `json:"evidence_refs"`
}

type Crowding struct {
	RuleVersion     string              `json:"rule_version"`
	Availability    string              `json:"availability"`
	Reason          string              `json:"reason,omitempty"`
	Symbol          string              `json:"symbol"`
	State           CrowdingState       `json:"state,omitempty"`
	Direction       CrowdingDirection   `json:"direction,omitempty"`
	AvailableInputs int                 `json:"available_inputs"`
	TriggerCount    int                 `json:"trigger_count"`
	Components      []CrowdingComponent `json:"components"`
	EvidenceRefs    []string            `json:"evidence_refs"`
}

type CrowdingInput struct {
	Symbol              string
	AsOf                string
	FundingHistory      []CrowdingFundingSample
	DailyBars           []DailyPriceBar
	CurrentOpenInterest string
	FundingReason       string
	DailyReason         string
	FundingEvidenceRefs []string
	DailyEvidenceRefs   []string
	OIEvidenceRefs      []string
}

func (crowding Crowding) Component(name string) CrowdingComponent {
	for _, component := range crowding.Components {
		if component.Name == name {
			return component
		}
	}
	return CrowdingComponent{}
}

func UnavailableCrowding(symbol, reason string) Crowding {
	components := []CrowdingComponent{
		unavailableCrowdingComponent(CrowdingInputFunding, reason, nil),
		unavailableCrowdingComponent(CrowdingInputOpenInterest, CrowdingReasonHistoricalOIUnavailable, nil),
		unavailableCrowdingComponent(CrowdingInputPremium, reason, nil),
		unavailableCrowdingComponent(CrowdingInputPriceExtension, reason, nil),
		unavailableCrowdingComponent(CrowdingInputVolume, reason, nil),
	}
	return Crowding{RuleVersion: CrowdingRuleVersion, Availability: AvailabilityUnavailable, Reason: reason, Symbol: symbol, Components: components, EvidenceRefs: []string{}}
}

func CalculateCrowding(input CrowdingInput) Crowding {
	result := UnavailableCrowding(input.Symbol, CrowdingReasonInvalidHistory)
	asOf, err := time.Parse(time.RFC3339Nano, input.AsOf)
	if err != nil || input.Symbol == "" {
		return result
	}
	asOf = asOf.UTC()
	funding, premium := fundingCrowdingComponents(input, asOf)
	extension, volume := dailyCrowdingComponents(input, asOf)
	oi := unavailableCrowdingComponent(CrowdingInputOpenInterest, CrowdingReasonHistoricalOIUnavailable, input.OIEvidenceRefs)
	if value, valueErr := parseDecimal(input.CurrentOpenInterest); valueErr == nil && value.Sign() > 0 && validateEvidenceRefs(input.OIEvidenceRefs) == nil {
		oi.CurrentValue = value.FloatString(8)
	}
	result.Components = []CrowdingComponent{
		funding,
		oi,
		premium,
		extension,
		volume,
	}
	result.EvidenceRefs = crowdingEvidenceRefs(result.Components)
	for _, component := range result.Components {
		if component.Availability == AvailabilityAvailable {
			result.AvailableInputs++
			if component.Triggered {
				result.TriggerCount++
			}
		}
	}
	if result.AvailableInputs < 4 {
		result.Reason = CrowdingReasonInsufficientInputs
		return result
	}
	result.Availability = AvailabilityAvailable
	result.Reason = ""
	result.State = crowdingState(result.TriggerCount, extension.CurrentValue)
	result.Direction = crowdingDirection(funding.Direction, premium.Direction, extension.Direction)
	return result
}

func fundingCrowdingComponents(input CrowdingInput, asOf time.Time) (CrowdingComponent, CrowdingComponent) {
	reason := input.FundingReason
	if reason == "" && validateEvidenceRefs(input.FundingEvidenceRefs) != nil {
		reason = CrowdingReasonInvalidHistory
	}
	if reason != "" {
		return unavailableCrowdingComponent(CrowdingInputFunding, reason, input.FundingEvidenceRefs), unavailableCrowdingComponent(CrowdingInputPremium, reason, input.FundingEvidenceRefs)
	}
	for index, sample := range input.FundingHistory {
		at, err := time.Parse(time.RFC3339Nano, sample.Time)
		if err != nil || at.After(asOf) || sample.Symbol != input.Symbol {
			return unavailableFundingPair(CrowdingReasonInvalidHistory, input.FundingEvidenceRefs)
		}
		if _, err := parseDecimal(sample.FundingRate); err != nil {
			return unavailableFundingPair(CrowdingReasonInvalidHistory, input.FundingEvidenceRefs)
		}
		if _, err := parseDecimal(sample.Premium); err != nil {
			return unavailableFundingPair(CrowdingReasonInvalidHistory, input.FundingEvidenceRefs)
		}
		if index > 0 {
			previous, _ := time.Parse(time.RFC3339Nano, input.FundingHistory[index-1].Time)
			if !at.UTC().Truncate(time.Hour).Equal(previous.UTC().Truncate(time.Hour).Add(time.Hour)) {
				return unavailableFundingPair(CrowdingReasonInvalidHistory, input.FundingEvidenceRefs)
			}
		}
	}
	if len(input.FundingHistory) < 720 {
		return unavailableCrowdingComponent(CrowdingInputFunding, CrowdingReasonInsufficientHistory, input.FundingEvidenceRefs), unavailableCrowdingComponent(CrowdingInputPremium, CrowdingReasonInsufficientHistory, input.FundingEvidenceRefs)
	}
	history := input.FundingHistory[len(input.FundingHistory)-720:]
	windowStart, _ := time.Parse(time.RFC3339Nano, history[0].Time)
	windowEnd, _ := time.Parse(time.RFC3339Nano, history[len(history)-1].Time)
	if asOf.Sub(windowEnd) > 90*time.Minute {
		funding, premium := unavailableFundingPair(CrowdingReasonStale, input.FundingEvidenceRefs)
		funding.Availability, premium.Availability = AvailabilityStale, AvailabilityStale
		return funding, premium
	}
	fundingValues := make([]string, len(history))
	premiumValues := make([]string, len(history))
	for index, sample := range history {
		fundingValues[index], premiumValues[index] = sample.FundingRate, sample.Premium
	}
	latest := history[len(history)-1]
	fundingPercentile, _ := empiricalPercentile(fundingValues, latest.FundingRate, true)
	premiumPercentile, _ := empiricalPercentile(premiumValues, latest.Premium, true)
	return availablePercentileComponent(CrowdingInputFunding, latest.FundingRate, fundingPercentile, windowStart, windowEnd, input.FundingEvidenceRefs),
		availablePercentileComponent(CrowdingInputPremium, latest.Premium, premiumPercentile, windowStart, windowEnd, input.FundingEvidenceRefs)
}

func dailyCrowdingComponents(input CrowdingInput, asOf time.Time) (CrowdingComponent, CrowdingComponent) {
	if input.DailyReason != "" {
		return unavailableCrowdingComponent(CrowdingInputPriceExtension, input.DailyReason, input.DailyEvidenceRefs), unavailableCrowdingComponent(CrowdingInputVolume, input.DailyReason, input.DailyEvidenceRefs)
	}
	if validateEvidenceRefs(input.DailyEvidenceRefs) != nil {
		return unavailableCrowdingComponent(CrowdingInputPriceExtension, CrowdingReasonInvalidHistory, input.DailyEvidenceRefs), unavailableCrowdingComponent(CrowdingInputVolume, CrowdingReasonInvalidHistory, input.DailyEvidenceRefs)
	}
	completed := make([]DailyPriceBar, 0, len(input.DailyBars))
	var previousOpen time.Time
	for index, bar := range input.DailyBars {
		openTime, closeTime, valid := validDailyPriceBar(bar, input.Symbol)
		if !valid || index > 0 && !openTime.Equal(previousOpen.Add(24*time.Hour)) {
			return unavailableCrowdingComponent(CrowdingInputPriceExtension, CrowdingReasonInvalidHistory, input.DailyEvidenceRefs), unavailableCrowdingComponent(CrowdingInputVolume, CrowdingReasonInvalidHistory, input.DailyEvidenceRefs)
		}
		previousOpen = openTime
		if closeTime.Before(asOf) {
			completed = append(completed, bar)
		}
	}
	if len(completed) > 0 {
		latestClose, _ := time.Parse(time.RFC3339Nano, completed[len(completed)-1].CloseTime)
		expectedClose := asOf.Truncate(24 * time.Hour).Add(-time.Millisecond)
		if !latestClose.Equal(expectedClose) {
			extension := unavailableCrowdingComponent(CrowdingInputPriceExtension, CrowdingReasonStale, input.DailyEvidenceRefs)
			volume := unavailableCrowdingComponent(CrowdingInputVolume, CrowdingReasonStale, input.DailyEvidenceRefs)
			extension.Availability, volume.Availability = AvailabilityStale, AvailabilityStale
			return extension, volume
		}
	}
	extension := unavailableCrowdingComponent(CrowdingInputPriceExtension, CrowdingReasonInsufficientHistory, input.DailyEvidenceRefs)
	volume := unavailableCrowdingComponent(CrowdingInputVolume, CrowdingReasonInsufficientHistory, input.DailyEvidenceRefs)
	if len(completed) >= 20 {
		window := completed[len(completed)-20:]
		values := make([]string, len(window))
		valid := true
		for index, bar := range window {
			value, _ := parseDecimal(bar.Volume)
			if value.Sign() <= 0 {
				valid = false
				break
			}
			values[index] = bar.Volume
		}
		if valid {
			current := window[len(window)-1].Volume
			percentile, _ := empiricalPercentile(values, current, false)
			start, _ := time.Parse(time.RFC3339Nano, window[0].OpenTime)
			end, _ := time.Parse(time.RFC3339Nano, window[len(window)-1].CloseTime)
			volume = availablePercentileComponent(CrowdingInputVolume, normalizeDecimal(current), percentile, start, end, input.DailyEvidenceRefs)
			volume.Direction = ""
		} else {
			volume.Reason = CrowdingReasonInvalidHistory
		}
	}
	if len(completed) >= 50 {
		closes := make([]*big.Rat, len(completed))
		highs := make([]*big.Rat, len(completed))
		lows := make([]*big.Rat, len(completed))
		for index, bar := range completed {
			closes[index], _ = parseDecimal(bar.Close)
			highs[index], _ = parseDecimal(bar.High)
			lows[index], _ = parseDecimal(bar.Low)
		}
		ema20 := exponentialMovingAverage(closes, 20)
		atr14 := averageTrueRange(highs, lows, closes, 14)
		if atr14.Sign() > 0 {
			value := new(big.Rat).Quo(new(big.Rat).Sub(closes[len(closes)-1], ema20), atr14)
			start, _ := time.Parse(time.RFC3339Nano, completed[0].OpenTime)
			end, _ := time.Parse(time.RFC3339Nano, completed[len(completed)-1].CloseTime)
			extension = CrowdingComponent{
				Name: CrowdingInputPriceExtension, Availability: AvailabilityAvailable,
				WindowStart: start.UTC().Format(time.RFC3339Nano), WindowEnd: end.UTC().Format(time.RFC3339Nano), CurrentValue: value.FloatString(8),
				Triggered: absolute(value).Cmp(big.NewRat(3, 2)) >= 0, Direction: directionFor(value), EvidenceRefs: slices.Clone(input.DailyEvidenceRefs),
			}
		} else {
			extension.Reason = CrowdingReasonInvalidHistory
		}
	}
	return extension, volume
}

func empiricalPercentile(values []string, current string, useAbsolute bool) (string, error) {
	currentValue, err := parseDecimal(current)
	if err != nil || len(values) == 0 {
		return "", errors.New("invalid percentile input")
	}
	if useAbsolute {
		currentValue.Abs(currentValue)
	}
	count := 0
	for _, raw := range values {
		value, err := parseDecimal(raw)
		if err != nil {
			return "", errors.New("invalid percentile history")
		}
		if useAbsolute {
			value.Abs(value)
		}
		if value.Cmp(currentValue) <= 0 {
			count++
		}
	}
	return new(big.Rat).Mul(big.NewRat(int64(count), int64(len(values))), big.NewRat(100, 1)).FloatString(8), nil
}

func crowdingState(triggerCount int, extension string) CrowdingState {
	if triggerCount == 5 {
		return CrowdingExtreme
	}
	if triggerCount == 4 {
		value, _ := parseDecimal(extension)
		if value != nil && absolute(value).Cmp(big.NewRat(5, 2)) >= 0 {
			return CrowdingExtreme
		}
		return CrowdingHigh
	}
	if triggerCount >= 3 {
		return CrowdingHigh
	}
	if triggerCount >= 1 {
		return CrowdingNormal
	}
	return CrowdingLow
}

func crowdingDirection(directions ...CrowdingDirection) CrowdingDirection {
	long, short := 0, 0
	for _, direction := range directions {
		switch direction {
		case CrowdingLong:
			long++
		case CrowdingShort:
			short++
		}
	}
	if long >= 2 {
		return CrowdingLong
	}
	if short >= 2 {
		return CrowdingShort
	}
	return CrowdingMixed
}

func availablePercentileComponent(name, current, percentile string, start, end time.Time, refs []string) CrowdingComponent {
	value, _ := parseDecimal(percentile)
	currentValue, _ := parseDecimal(current)
	return CrowdingComponent{
		Name: name, Availability: AvailabilityAvailable,
		WindowStart: start.UTC().Format(time.RFC3339Nano), WindowEnd: end.UTC().Format(time.RFC3339Nano),
		CurrentValue: normalizeDecimal(current), Percentile: percentile, Triggered: value.Cmp(big.NewRat(75, 1)) >= 0,
		Direction: directionFor(currentValue), EvidenceRefs: slices.Clone(refs),
	}
}

func unavailableFundingPair(reason string, refs []string) (CrowdingComponent, CrowdingComponent) {
	return unavailableCrowdingComponent(CrowdingInputFunding, reason, refs), unavailableCrowdingComponent(CrowdingInputPremium, reason, refs)
}

func unavailableCrowdingComponent(name, reason string, refs []string) CrowdingComponent {
	return CrowdingComponent{Name: name, Availability: AvailabilityUnavailable, Reason: reason, EvidenceRefs: append([]string{}, refs...)}
}

func crowdingEvidenceRefs(components []CrowdingComponent) []string {
	refs := make([]string, 0)
	for _, component := range components {
		refs = append(refs, component.EvidenceRefs...)
	}
	slices.Sort(refs)
	return slices.Compact(refs)
}

func directionFor(value *big.Rat) CrowdingDirection {
	if value.Sign() > 0 {
		return CrowdingLong
	}
	if value.Sign() < 0 {
		return CrowdingShort
	}
	return ""
}

func absolute(value *big.Rat) *big.Rat {
	return new(big.Rat).Abs(new(big.Rat).Set(value))
}

func normalizeDecimal(raw string) string {
	value, _ := parseDecimal(raw)
	return value.FloatString(8)
}
