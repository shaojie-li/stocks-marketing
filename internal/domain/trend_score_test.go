package domain

import (
	"math/big"
	"testing"
)

func TestCalculateTrendScoreMapsAllComponents(t *testing.T) {
	result, err := CalculateTrendScore(TrendScoreInput{
		Indicators:         scoredCoreIndicators(t),
		Phase:              "WEEKEND",
		PriceStructure:     PriceStructureAboveSupport,
		ForeignFlow:        ForeignFlowNetBuy,
		PriceStructureRefs: []string{"ev-price-structure"},
		ForeignFlowRefs:    []string{"ev-foreign-flow"},
	})
	if err != nil {
		t.Fatalf("calculate Trend Score: %v", err)
	}
	if result.Value != "9.5" || result.CoveragePct != 100 || result.ConfidenceMax != ConfidenceHigh {
		t.Fatalf("Trend Score = %#v, want value 9.5 at full coverage", result)
	}
	if len(result.Components) != 8 || result.Components[0].Name != IndicatorMemoryRelativeStrength || result.Components[0].Value != "2.0" {
		t.Fatalf("Trend components were not audited: %#v", result.Components)
	}
	if len(result.Components[0].EvidenceRefs) != 2 || result.Components[6].EvidenceRefs[0] != "ev-price-structure" {
		t.Fatalf("Trend evidence references are incomplete: %#v", result.Components)
	}
	wantValues := []string{"2.0", "0.8", "0.7", "2.0", "1.0", "1.0", "1.0", "1.0"}
	for index, want := range wantValues {
		if result.Components[index].Value != want {
			t.Errorf("component %s value = %s, want %s", result.Components[index].Name, result.Components[index].Value, want)
		}
	}
}

func TestCalculateTrendScoreNormalizesAvailablePriceComponents(t *testing.T) {
	result, err := CalculateTrendScore(TrendScoreInput{Indicators: scoredCoreIndicators(t), Phase: "WEEKEND"})
	if err != nil {
		t.Fatalf("calculate Trend Score: %v", err)
	}
	if result.Value != "9.4" || result.CoveragePct != 80 || result.ConfidenceMax != ConfidenceMedium {
		t.Fatalf("price-only Trend Score = %#v, want 9.4/80%%/MEDIUM", result)
	}
	if result.Components[6].Availability != AvailabilityUnavailable || result.Components[6].Value != "" || result.Components[7].Availability != AvailabilityUnavailable {
		t.Fatalf("missing components were converted into a signal: %#v", result.Components)
	}
	componentTotal := new(big.Rat)
	for _, component := range result.Components {
		if component.Value == "" {
			continue
		}
		value, err := parseDecimal(component.Value)
		if err != nil {
			t.Fatal(err)
		}
		componentTotal.Add(componentTotal, value)
	}
	if componentTotal.FloatString(1) != result.Value {
		t.Fatalf("normalized components total %s, want Score %s", componentTotal.FloatString(1), result.Value)
	}
}

func TestCalculateTrendScoreFailsClosedBelowSeventyPercentCoverage(t *testing.T) {
	core := scoredCoreIndicators(t)
	delete(core.Relative, IndicatorMemoryRelativeStrength)
	result, err := CalculateTrendScore(TrendScoreInput{Indicators: core, Phase: "WEEKEND"})
	if err != nil {
		t.Fatalf("calculate partial Trend Score: %v", err)
	}
	if result.CoveragePct != 60 || result.Value != "" || result.Direction != "" || result.ConfidenceMax != ConfidenceLow {
		t.Fatalf("insufficient coverage produced a Score: %#v", result)
	}
}

func TestCalculateTrendScoreRemainsAvailableAtSeventyPercentCoverage(t *testing.T) {
	core := scoredCoreIndicators(t)
	delete(core.Relative, IndicatorAIComputeRotation)
	result, err := CalculateTrendScore(TrendScoreInput{Indicators: core, Phase: "WEEKEND"})
	if err != nil {
		t.Fatalf("calculate 70%% Trend Score: %v", err)
	}
	if result.CoveragePct != 70 || result.Value == "" || result.ConfidenceMax != ConfidenceMedium {
		t.Fatalf("70%% coverage was not available: %#v", result)
	}
}

func TestTrendDirectionRequiresComparableContextAndExactBoundaries(t *testing.T) {
	tests := map[string]struct {
		previous string
		current  string
		want     ScoreDirection
	}{
		"up boundary":         {"8.0", "8.5", ScoreDirectionUp},
		"below up boundary":   {"8.0", "8.4999", ScoreDirectionFlat},
		"down boundary":       {"8.0", "7.5", ScoreDirectionDown},
		"above down boundary": {"8.0", "7.5001", ScoreDirectionFlat},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := classifyScoreDirection(test.previous, test.current)
			if err != nil || got != test.want {
				t.Fatalf("direction = %s, %v; want %s", got, err, test.want)
			}
		})
	}

	previous, err := CalculateTrendScore(TrendScoreInput{Indicators: scoredCoreIndicators(t), Phase: "CLOSE"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := CalculateTrendScore(TrendScoreInput{Indicators: scoredCoreIndicators(t), Phase: "WEEKEND", Previous: &previous})
	if err != nil {
		t.Fatal(err)
	}
	if current.Direction != "" {
		t.Fatalf("incomparable phase produced direction %s", current.Direction)
	}

	previous, err = CalculateTrendScore(TrendScoreInput{
		Indicators: scoredCoreIndicators(t), Phase: "WEEKEND",
		PriceStructure: PriceStructureRange, ForeignFlow: ForeignFlowNeutral,
		PriceStructureRefs: []string{"ev-price-structure"}, ForeignFlowRefs: []string{"ev-foreign-flow"},
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err = CalculateTrendScore(TrendScoreInput{
		Indicators: scoredCoreIndicators(t), Phase: "WEEKEND",
		PriceStructure: PriceStructureAboveSupport, ForeignFlow: ForeignFlowNetBuy,
		PriceStructureRefs: []string{"ev-price-structure"}, ForeignFlowRefs: []string{"ev-foreign-flow"},
		Previous: &previous,
	})
	if err != nil {
		t.Fatal(err)
	}
	if current.Direction != ScoreDirectionUp {
		t.Fatalf("comparable score direction = %s, want UP", current.Direction)
	}

	priceOnly, err := CalculateTrendScore(TrendScoreInput{Indicators: scoredCoreIndicators(t), Phase: "WEEKEND", Previous: &previous})
	if err != nil {
		t.Fatal(err)
	}
	if priceOnly.Direction != "" {
		t.Fatalf("changed component set produced direction %s", priceOnly.Direction)
	}
}

func TestCalculateTrendScoreRejectsAvailableAuxiliaryWithoutEvidence(t *testing.T) {
	if _, err := CalculateTrendScore(TrendScoreInput{
		Indicators: scoredCoreIndicators(t), Phase: "WEEKEND", PriceStructure: PriceStructureRange,
	}); err == nil {
		t.Fatal("available price structure without evidence was accepted")
	}
}

func TestTrendComponentStateMappings(t *testing.T) {
	relative := map[RelativeStrengthState]string{
		RelativeStrong: "1.00", RelativePositive: "0.75", RelativeNeutral: "0.50", RelativeWeak: "0.00",
	}
	for state, want := range relative {
		fraction, available, err := trendComponentFraction(IndicatorMemoryRelativeStrength, TrendScoreInput{Indicators: CoreIndicatorSet{Relative: map[string]RelativeIndicator{
			IndicatorMemoryRelativeStrength: {Availability: AvailabilityAvailable, State: state},
		}}})
		if err != nil || !available || fraction.FloatString(2) != want {
			t.Errorf("Relative state %s = %v/%v/%v, want %s", state, fraction, available, err, want)
		}
	}
	cross := []struct {
		state     CrossMarketState
		direction MarketDirection
		want      string
	}{
		{CrossMarketConfirmed, DirectionBullish, "1.00"},
		{CrossMarketNeutral, DirectionNone, "0.50"},
		{CrossMarketDivergence, DirectionNone, "0.00"},
		{CrossMarketConfirmed, DirectionBearish, "0.00"},
	}
	for _, test := range cross {
		fraction, available, err := crossMarketTrendFraction(CrossMarketIndicator{Availability: AvailabilityAvailable, State: test.state, Direction: test.direction})
		if err != nil || !available || fraction.FloatString(2) != test.want {
			t.Errorf("Cross-Market %s/%s = %v/%v/%v, want %s", test.state, test.direction, fraction, available, err, test.want)
		}
	}
}

func scoredCoreIndicators(t *testing.T) CoreIndicatorSet {
	t.Helper()
	result, err := CalculateCoreIndicators(coreObservations())
	if err != nil {
		t.Fatalf("calculate fixture core indicators: %v", err)
	}
	return result
}
