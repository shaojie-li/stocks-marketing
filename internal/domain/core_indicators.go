package domain

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"
)

const (
	IndicatorMemoryRelativeStrength        = "memory_relative_strength"
	IndicatorSemiconductorRelativeStrength = "semiconductor_relative_strength"
	IndicatorAIComputeRotation             = "ai_compute_rotation"
	IndicatorSKHYSectorAlpha               = "skhy_sector_alpha"
	IndicatorSKHYMarketAlpha               = "skhy_market_alpha"
)

var (
	decimalStringPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
	indicatorPairs       = map[string][2]string{
		IndicatorMemoryRelativeStrength:        {"xyz:MU", "xyz:SMH"},
		IndicatorSemiconductorRelativeStrength: {"xyz:SMH", "xyz:XYZ100"},
		IndicatorAIComputeRotation:             {"xyz:AMD", "xyz:NVDA"},
		IndicatorSKHYSectorAlpha:               {"xyz:SKHY", "xyz:SMSN"},
		IndicatorSKHYMarketAlpha:               {"xyz:SKHY", "xyz:KR200"},
	}
)

type RelativeStrengthState string

const (
	RelativeStrong   RelativeStrengthState = "STRONG"
	RelativePositive RelativeStrengthState = "POSITIVE"
	RelativeNeutral  RelativeStrengthState = "NEUTRAL"
	RelativeWeak     RelativeStrengthState = "WEAK"
)

type CrossMarketState string

const (
	CrossMarketConfirmed  CrossMarketState = "CONFIRMED"
	CrossMarketNeutral    CrossMarketState = "NEUTRAL"
	CrossMarketDivergence CrossMarketState = "DIVERGENCE"
)

type MarketDirection string

const (
	DirectionBullish MarketDirection = "BULLISH"
	DirectionBearish MarketDirection = "BEARISH"
	DirectionNone    MarketDirection = "NONE"
)

type RelativeIndicator struct {
	Availability string                `json:"availability"`
	ValuePP      string                `json:"value_pp"`
	State        RelativeStrengthState `json:"state"`
	WindowType   string                `json:"window_type"`
	WindowStart  string                `json:"window_start"`
	WindowEnd    string                `json:"window_end"`
	SessionDate  string                `json:"session_date"`
	LeftSymbol   string                `json:"left_symbol"`
	RightSymbol  string                `json:"right_symbol"`
	InputRefs    []string              `json:"input_observation_refs"`
}

type CrossMarketIndicator struct {
	Availability  string           `json:"availability"`
	State         CrossMarketState `json:"state"`
	Direction     MarketDirection  `json:"direction"`
	WindowType    string           `json:"window_type"`
	WindowStart   string           `json:"window_start"`
	WindowEnd     string           `json:"window_end"`
	USSessionDate string           `json:"us_session_date"`
	KRSessionDate string           `json:"kr_session_date"`
	LagHours      float64          `json:"lag_hours"`
	EvidenceRefs  []string         `json:"evidence_refs"`
}

type CoreIndicatorSet struct {
	RuleVersion string                       `json:"rule_version"`
	Relative    map[string]RelativeIndicator `json:"relative"`
	CrossMarket CrossMarketIndicator         `json:"cross_market"`
}

func CalculateCoreIndicators(observations []Observation) (CoreIndicatorSet, error) {
	bySymbol := make(map[string]Observation, len(observations))
	returns := make(map[string]*big.Rat, len(observations))
	for _, observation := range observations {
		if observation.Symbol == "" {
			return CoreIndicatorSet{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: observation symbol is empty")
		}
		if _, exists := bySymbol[observation.Symbol]; exists {
			return CoreIndicatorSet{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: duplicate observation %s", observation.Symbol)
		}
		if err := validateObservation(observation); err != nil {
			return CoreIndicatorSet{}, err
		}
		value, err := parseDecimal(observation.ChangePct)
		if err != nil {
			return CoreIndicatorSet{}, fmt.Errorf("invalid return for %s: %w", observation.Symbol, err)
		}
		bySymbol[observation.Symbol] = observation
		returns[observation.Symbol] = value
	}

	result := CoreIndicatorSet{RuleVersion: "global-analysis/1.1.0", Relative: make(map[string]RelativeIndicator, len(indicatorPairs))}
	for name, pair := range indicatorPairs {
		left, leftOK := bySymbol[pair[0]]
		right, rightOK := bySymbol[pair[1]]
		if !leftOK || !rightOK {
			return CoreIndicatorSet{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: core observation missing for %s", name)
		}
		if err := validateComparable(left, right); err != nil {
			return CoreIndicatorSet{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: %s: %w", name, err)
		}
		value := new(big.Rat).Sub(returns[pair[0]], returns[pair[1]])
		state := classifyRelativeRat(value)
		result.Relative[name] = RelativeIndicator{
			Availability: "AVAILABLE", ValuePP: value.FloatString(8), State: state,
			WindowType: left.WindowType, WindowStart: left.WindowStart, WindowEnd: left.WindowEnd,
			SessionDate: left.SessionDate, LeftSymbol: pair[0], RightSymbol: pair[1],
			InputRefs: []string{left.ID, right.ID},
		}
	}

	memory := result.Relative[IndicatorMemoryRelativeStrength]
	skhySector := result.Relative[IndicatorSKHYSectorAlpha]
	crossState, direction := classifyCrossMarket(memory.State, skhySector.State)
	result.CrossMarket = CrossMarketIndicator{
		Availability: "AVAILABLE", State: crossState, Direction: direction,
		WindowType: memory.WindowType, WindowStart: memory.WindowStart, WindowEnd: memory.WindowEnd,
		USSessionDate: memory.SessionDate, KRSessionDate: skhySector.SessionDate, LagHours: 0,
		EvidenceRefs: []string{IndicatorMemoryRelativeStrength, IndicatorSKHYSectorAlpha},
	}
	return result, nil
}

func ClassifyRelativeStrength(value string) (RelativeStrengthState, error) {
	parsed, err := parseDecimal(value)
	if err != nil {
		return "", err
	}
	return classifyRelativeRat(parsed), nil
}

func validateObservation(observation Observation) error {
	if observation.Freshness != "REALTIME" && observation.Freshness != "DELAYED" && observation.Freshness != "EOD" {
		return fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: stale observation %s", observation.Symbol)
	}
	if observation.WindowType == "" || observation.WindowStart == "" || observation.WindowEnd == "" || observation.SessionDate == "" || observation.Source == "" || observation.SourceTier == "" || observation.Adjustment == "" {
		return fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: incomplete observation %s", observation.Symbol)
	}
	price, err := parseDecimal(observation.Price)
	if err != nil || price.Sign() <= 0 || observation.MarketStatus == "" {
		return fmt.Errorf("invalid observation price or market status for %s", observation.Symbol)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, observation.ObservedAt)
	if err != nil {
		return fmt.Errorf("invalid observation time for %s", observation.Symbol)
	}
	windowStart, err := time.Parse(time.RFC3339Nano, observation.WindowStart)
	if err != nil {
		return fmt.Errorf("invalid window start for %s", observation.Symbol)
	}
	windowEnd, err := time.Parse(time.RFC3339Nano, observation.WindowEnd)
	if err != nil || !windowStart.Before(windowEnd) || !observedAt.Equal(windowEnd) {
		return fmt.Errorf("invalid effective window for %s", observation.Symbol)
	}
	if _, err := time.Parse(time.DateOnly, observation.SessionDate); err != nil {
		return fmt.Errorf("invalid session date for %s", observation.Symbol)
	}
	if observation.BaselinePrice != "" || observation.BaselineAt != "" {
		baseline, baselineErr := parseDecimal(observation.BaselinePrice)
		baselineAt, timeErr := time.Parse(time.RFC3339Nano, observation.BaselineAt)
		if baselineErr != nil || timeErr != nil || baseline.Sign() <= 0 || !baselineAt.Equal(windowStart) {
			return fmt.Errorf("invalid baseline audit for %s", observation.Symbol)
		}
	}
	if observation.TheoreticalStart != "" {
		theoreticalStart, theoreticalErr := time.Parse(time.RFC3339Nano, observation.TheoreticalStart)
		anchorLag := theoreticalStart.Sub(windowStart)
		if theoreticalErr != nil || anchorLag <= 0 || anchorLag >= time.Minute {
			return fmt.Errorf("invalid theoretical window audit for %s", observation.Symbol)
		}
	}
	return nil
}

func validateComparable(left, right Observation) error {
	if left.WindowType != right.WindowType || left.WindowStart != right.WindowStart || left.WindowEnd != right.WindowEnd || left.SessionDate != right.SessionDate {
		return errors.New("indicator window mismatch")
	}
	if left.MarketStatus != right.MarketStatus || left.Source != right.Source || left.SourceTier != right.SourceTier || left.Adjustment != right.Adjustment {
		return errors.New("indicator observation semantics mismatch")
	}
	if left.TheoreticalStart != right.TheoreticalStart {
		return errors.New("indicator theoretical window mismatch")
	}
	return nil
}

func classifyRelativeRat(value *big.Rat) RelativeStrengthState {
	if value.Cmp(big.NewRat(1, 1)) >= 0 {
		return RelativeStrong
	}
	if value.Cmp(big.NewRat(1, 4)) >= 0 {
		return RelativePositive
	}
	if value.Cmp(big.NewRat(-1, 4)) <= 0 {
		return RelativeWeak
	}
	return RelativeNeutral
}

func classifyCrossMarket(left, right RelativeStrengthState) (CrossMarketState, MarketDirection) {
	leftDirection, rightDirection := relativeDirection(left), relativeDirection(right)
	if leftDirection == 0 || rightDirection == 0 {
		return CrossMarketNeutral, DirectionNone
	}
	if leftDirection != rightDirection {
		return CrossMarketDivergence, DirectionNone
	}
	if leftDirection > 0 {
		return CrossMarketConfirmed, DirectionBullish
	}
	return CrossMarketConfirmed, DirectionBearish
}

func relativeDirection(state RelativeStrengthState) int {
	switch state {
	case RelativeStrong, RelativePositive:
		return 1
	case RelativeWeak:
		return -1
	default:
		return 0
	}
}

func parseDecimal(value string) (*big.Rat, error) {
	if !decimalStringPattern.MatchString(value) {
		return nil, errors.New("value is not a decimal string")
	}
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, errors.New("value is not a decimal")
	}
	return parsed, nil
}
