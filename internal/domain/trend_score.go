package domain

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
)

const (
	AvailabilityAvailable   = "AVAILABLE"
	AvailabilityUnavailable = "UNAVAILABLE"

	PriceStructureAboveSupport PriceStructureState = "ABOVE_SUPPORT"
	PriceStructureRange        PriceStructureState = "RANGE"
	PriceStructureBroken       PriceStructureState = "BROKEN"

	ForeignFlowNetBuy  ForeignFlowDirection = "NET_BUY"
	ForeignFlowNeutral ForeignFlowDirection = "NEUTRAL"
	ForeignFlowNetSell ForeignFlowDirection = "NET_SELL"

	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"

	ScoreDirectionUp   ScoreDirection = "UP"
	ScoreDirectionFlat ScoreDirection = "FLAT"
	ScoreDirectionDown ScoreDirection = "DOWN"
)

type PriceStructureState string
type ForeignFlowDirection string
type Confidence string
type ScoreDirection string

type TrendComponent struct {
	Name         string   `json:"name"`
	Weight       int      `json:"weight"`
	Availability string   `json:"availability"`
	Value        string   `json:"value,omitempty"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type TrendScore struct {
	RuleVersion   string           `json:"rule_version"`
	Phase         string           `json:"phase"`
	WindowType    string           `json:"window_type"`
	Value         string           `json:"value,omitempty"`
	Direction     ScoreDirection   `json:"direction,omitempty"`
	CoveragePct   int              `json:"coverage_pct"`
	ConfidenceMax Confidence       `json:"confidence_max"`
	Components    []TrendComponent `json:"components"`
}

type TrendScoreInput struct {
	Indicators         CoreIndicatorSet
	Phase              string
	PriceStructure     PriceStructureState
	ForeignFlow        ForeignFlowDirection
	PriceStructureRefs []string
	ForeignFlowRefs    []string
	Previous           *TrendScore
}

type trendComponentSpec struct {
	name   string
	weight int
}

var trendComponentSpecs = []trendComponentSpec{
	{IndicatorMemoryRelativeStrength, 2},
	{IndicatorSemiconductorRelativeStrength, 1},
	{IndicatorAIComputeRotation, 1},
	{IndicatorSKHYSectorAlpha, 2},
	{IndicatorSKHYMarketAlpha, 1},
	{"cross_market", 1},
	{"skhy_price_structure", 1},
	{"skhy_foreign_flow", 1},
}

func CalculateTrendScore(input TrendScoreInput) (TrendScore, error) {
	if input.Indicators.RuleVersion == "" || input.Indicators.CrossMarket.WindowType == "" || input.Phase == "" {
		return TrendScore{}, errors.New("Trend Score context is incomplete")
	}
	result := TrendScore{
		RuleVersion: input.Indicators.RuleVersion,
		Phase:       input.Phase,
		WindowType:  input.Indicators.CrossMarket.WindowType,
		Components:  make([]TrendComponent, 0, len(trendComponentSpecs)),
	}
	earned := new(big.Rat)
	availableWeight := 0
	exactValues := make([]*big.Rat, 0, len(trendComponentSpecs))
	for _, spec := range trendComponentSpecs {
		component := TrendComponent{Name: spec.name, Weight: spec.weight, Availability: AvailabilityUnavailable, EvidenceRefs: []string{}}
		fraction, available, err := trendComponentFraction(spec.name, input)
		if err != nil {
			return TrendScore{}, err
		}
		if available {
			evidenceRefs := trendComponentEvidenceRefs(spec.name, input)
			if err := validateEvidenceRefs(evidenceRefs); err != nil {
				return TrendScore{}, fmt.Errorf("%s evidence: %w", spec.name, err)
			}
			component.Availability = AvailabilityAvailable
			component.EvidenceRefs = evidenceRefs
			value := new(big.Rat).Mul(fraction, big.NewRat(int64(spec.weight), 1))
			component.Value = value.FloatString(2)
			earned.Add(earned, value)
			availableWeight += spec.weight
			exactValues = append(exactValues, value)
		} else {
			exactValues = append(exactValues, nil)
		}
		result.Components = append(result.Components, component)
	}
	result.CoveragePct = availableWeight * 10
	switch {
	case result.CoveragePct == 100:
		result.ConfidenceMax = ConfidenceHigh
	case result.CoveragePct >= 70:
		result.ConfidenceMax = ConfidenceMedium
	default:
		result.ConfidenceMax = ConfidenceLow
		return result, nil
	}
	score := new(big.Rat).Mul(earned, big.NewRat(10, int64(availableWeight)))
	result.Value = score.FloatString(1)
	if err := normalizeTrendComponents(result.Components, exactValues, availableWeight, result.Value); err != nil {
		return TrendScore{}, err
	}
	if input.Previous != nil && comparableTrendScore(*input.Previous, result) {
		direction, err := classifyScoreDirection(input.Previous.Value, result.Value)
		if err != nil {
			return TrendScore{}, err
		}
		result.Direction = direction
	}
	return result, nil
}

func normalizeTrendComponents(components []TrendComponent, exactValues []*big.Rat, availableWeight int, scoreValue string) error {
	target, err := parseDecimal(scoreValue)
	if err != nil {
		return err
	}
	target.Mul(target, big.NewRat(10, 1))
	if !target.IsInt() {
		return errors.New("Trend Score does not have one-decimal precision")
	}
	targetTenths := target.Num().Int64()
	type allocation struct {
		index     int
		tenths    int64
		remainder *big.Rat
	}
	allocations := make([]allocation, 0, len(components))
	allocatedTenths := int64(0)
	for index, exact := range exactValues {
		if exact == nil {
			continue
		}
		scaled := new(big.Rat).Mul(exact, big.NewRat(100, int64(availableWeight)))
		floor := new(big.Int).Quo(scaled.Num(), scaled.Denom()).Int64()
		remainder := new(big.Rat).Sub(scaled, big.NewRat(floor, 1))
		allocations = append(allocations, allocation{index: index, tenths: floor, remainder: remainder})
		allocatedTenths += floor
	}
	sort.SliceStable(allocations, func(left, right int) bool {
		return allocations[left].remainder.Cmp(allocations[right].remainder) > 0
	})
	remaining := targetTenths - allocatedTenths
	if remaining < 0 || remaining > int64(len(allocations)) {
		return errors.New("cannot reconcile Trend component rounding")
	}
	for index := int64(0); index < remaining; index++ {
		allocations[index].tenths++
	}
	for _, item := range allocations {
		components[item.index].Value = fmt.Sprintf("%d.%d", item.tenths/10, item.tenths%10)
	}
	return nil
}

func trendComponentEvidenceRefs(name string, input TrendScoreInput) []string {
	switch name {
	case "cross_market":
		return input.Indicators.CrossMarket.EvidenceRefs
	case "skhy_price_structure":
		return input.PriceStructureRefs
	case "skhy_foreign_flow":
		return input.ForeignFlowRefs
	default:
		return input.Indicators.Relative[name].InputRefs
	}
}

func validateEvidenceRefs(refs []string) error {
	if len(refs) == 0 {
		return errors.New("evidence references are empty")
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref == "" {
			return errors.New("evidence reference is empty")
		}
		if _, exists := seen[ref]; exists {
			return errors.New("evidence references contain a duplicate")
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func trendComponentFraction(name string, input TrendScoreInput) (*big.Rat, bool, error) {
	if name == "cross_market" {
		return crossMarketTrendFraction(input.Indicators.CrossMarket)
	}
	if name == "skhy_price_structure" {
		return priceStructureFraction(input.PriceStructure)
	}
	if name == "skhy_foreign_flow" {
		return foreignFlowFraction(input.ForeignFlow)
	}
	indicator, ok := input.Indicators.Relative[name]
	if !ok || indicator.Availability != AvailabilityAvailable {
		return nil, false, nil
	}
	switch indicator.State {
	case RelativeStrong:
		return big.NewRat(1, 1), true, nil
	case RelativePositive:
		return big.NewRat(3, 4), true, nil
	case RelativeNeutral:
		return big.NewRat(1, 2), true, nil
	case RelativeWeak:
		return new(big.Rat), true, nil
	default:
		return nil, false, fmt.Errorf("invalid Relative Strength state for %s", name)
	}
}

func crossMarketTrendFraction(indicator CrossMarketIndicator) (*big.Rat, bool, error) {
	if indicator.Availability != AvailabilityAvailable {
		return nil, false, nil
	}
	switch {
	case indicator.State == CrossMarketConfirmed && indicator.Direction == DirectionBullish:
		return big.NewRat(1, 1), true, nil
	case indicator.State == CrossMarketNeutral && indicator.Direction == DirectionNone:
		return big.NewRat(1, 2), true, nil
	case indicator.State == CrossMarketDivergence && indicator.Direction == DirectionNone:
		return new(big.Rat), true, nil
	case indicator.State == CrossMarketConfirmed && indicator.Direction == DirectionBearish:
		return new(big.Rat), true, nil
	default:
		return nil, false, errors.New("invalid Cross-Market state and direction")
	}
}

func priceStructureFraction(state PriceStructureState) (*big.Rat, bool, error) {
	switch state {
	case "":
		return nil, false, nil
	case PriceStructureAboveSupport:
		return big.NewRat(1, 1), true, nil
	case PriceStructureRange:
		return big.NewRat(1, 2), true, nil
	case PriceStructureBroken:
		return new(big.Rat), true, nil
	default:
		return nil, false, errors.New("invalid SKHY price structure")
	}
}

func foreignFlowFraction(direction ForeignFlowDirection) (*big.Rat, bool, error) {
	switch direction {
	case "":
		return nil, false, nil
	case ForeignFlowNetBuy:
		return big.NewRat(1, 1), true, nil
	case ForeignFlowNeutral:
		return big.NewRat(1, 2), true, nil
	case ForeignFlowNetSell:
		return new(big.Rat), true, nil
	default:
		return nil, false, errors.New("invalid Foreign Flow direction")
	}
}

func comparableTrendScore(previous, current TrendScore) bool {
	if previous.Value == "" || previous.RuleVersion != current.RuleVersion || previous.Phase != current.Phase || previous.WindowType != current.WindowType {
		return false
	}
	return slices.Equal(availableTrendComponents(previous), availableTrendComponents(current))
}

func availableTrendComponents(score TrendScore) []string {
	result := make([]string, 0, len(score.Components))
	for _, component := range score.Components {
		if component.Availability == AvailabilityAvailable {
			result = append(result, component.Name)
		}
	}
	return result
}

func classifyScoreDirection(previous, current string) (ScoreDirection, error) {
	previousValue, err := parseDecimal(previous)
	if err != nil {
		return "", err
	}
	currentValue, err := parseDecimal(current)
	if err != nil {
		return "", err
	}
	delta := new(big.Rat).Sub(currentValue, previousValue)
	if delta.Cmp(big.NewRat(1, 2)) >= 0 {
		return ScoreDirectionUp, nil
	}
	if delta.Cmp(big.NewRat(-1, 2)) <= 0 {
		return ScoreDirectionDown, nil
	}
	return ScoreDirectionFlat, nil
}
