package domain

import "testing"

func TestCalculateCoreIndicatorsUsesOneWindowAndComputesCrossMarket(t *testing.T) {
	observations := coreObservations()
	result, err := CalculateCoreIndicators(observations)
	if err != nil {
		t.Fatalf("calculate indicators: %v", err)
	}
	want := map[string]struct {
		value string
		state RelativeStrengthState
	}{
		IndicatorMemoryRelativeStrength:        {"1.30000000", RelativeStrong},
		IndicatorSemiconductorRelativeStrength: {"0.70000000", RelativePositive},
		IndicatorAIComputeRotation:             {"0.30000000", RelativePositive},
		IndicatorSKHYSectorAlpha:               {"2.00000000", RelativeStrong},
		IndicatorSKHYMarketAlpha:               {"2.20000000", RelativeStrong},
	}
	for name, expected := range want {
		indicator := result.Relative[name]
		if indicator.ValuePP != expected.value || indicator.State != expected.state {
			t.Errorf("%s = %#v, want value=%s state=%s", name, indicator, expected.value, expected.state)
		}
		if indicator.WindowStart != "2026-08-14T06:00:00Z" || indicator.WindowEnd != "2026-08-15T06:00:00Z" {
			t.Errorf("%s lost effective window: %#v", name, indicator)
		}
	}
	if result.CrossMarket.State != CrossMarketConfirmed || result.CrossMarket.Direction != DirectionBullish {
		t.Fatalf("cross market = %#v, want bullish confirmation", result.CrossMarket)
	}
	if result.CrossMarket.USSessionDate != "2026-08-15" || result.CrossMarket.KRSessionDate != "2026-08-15" || result.CrossMarket.LagHours != 0 {
		t.Fatalf("cross-market timing audit is incomplete: %#v", result.CrossMarket)
	}
}

func TestRelativeStrengthBoundaryClassification(t *testing.T) {
	tests := map[string]RelativeStrengthState{
		"1.00": RelativeStrong, "0.9999": RelativePositive,
		"0.25": RelativePositive, "0.2499": RelativeNeutral,
		"0.00": RelativeNeutral, "-0.2499": RelativeNeutral,
		"-0.25": RelativeWeak,
	}
	for value, expected := range tests {
		state, err := ClassifyRelativeStrength(value)
		if err != nil || state != expected {
			t.Errorf("classify %s = %s, %v; want %s", value, state, err, expected)
		}
	}
	if _, err := ClassifyRelativeStrength("1/2"); err == nil {
		t.Fatal("fraction syntax was accepted as decimal")
	}
}

func TestCrossMarketClassification(t *testing.T) {
	tests := []struct {
		left, right RelativeStrengthState
		state       CrossMarketState
		direction   MarketDirection
	}{
		{RelativeStrong, RelativePositive, CrossMarketConfirmed, DirectionBullish},
		{RelativeWeak, RelativeWeak, CrossMarketConfirmed, DirectionBearish},
		{RelativePositive, RelativeWeak, CrossMarketDivergence, DirectionNone},
		{RelativeNeutral, RelativeStrong, CrossMarketNeutral, DirectionNone},
	}
	for _, test := range tests {
		state, direction := classifyCrossMarket(test.left, test.right)
		if state != test.state || direction != test.direction {
			t.Errorf("cross market %s/%s = %s/%s, want %s/%s", test.left, test.right, state, direction, test.state, test.direction)
		}
	}
}

func TestCalculateCoreIndicatorsFailsClosedOnMissingOrMismatchedWindow(t *testing.T) {
	tests := map[string]func([]Observation) []Observation{
		"missing": func(input []Observation) []Observation { return input[:len(input)-1] },
		"window mismatch": func(input []Observation) []Observation {
			input[1].WindowEnd = "2026-08-15T05:59:59Z"
			return input
		},
		"stale": func(input []Observation) []Observation {
			input[2].Freshness = "STALE"
			return input
		},
		"duplicate": func(input []Observation) []Observation { return append(input, input[0]) },
		"baseline mismatch": func(input []Observation) []Observation {
			input[0].BaselinePrice = "99"
			input[0].BaselineAt = "2026-08-14T05:59:00Z"
			return input
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := CalculateCoreIndicators(mutate(coreObservations())); err == nil {
				t.Fatal("invalid core observations were accepted")
			}
		})
	}
}

func coreObservations() []Observation {
	returns := map[string]string{
		"xyz:MU": "2.40", "xyz:SMH": "1.10", "xyz:XYZ100": "0.40",
		"xyz:AMD": "1.50", "xyz:NVDA": "1.20", "xyz:SKHY": "3.00",
		"xyz:SMSN": "1.00", "xyz:KR200": "0.80",
	}
	result := make([]Observation, 0, len(returns))
	for symbol, change := range returns {
		result = append(result, Observation{
			ID: symbol, Symbol: symbol, Price: "100", ChangePct: change,
			ObservedAt: "2026-08-15T06:00:00Z", MarketStatus: "CONTINUOUS",
			WindowType: "CONTRACT_24H", WindowStart: "2026-08-14T06:00:00Z", WindowEnd: "2026-08-15T06:00:00Z",
			SessionDate: "2026-08-15", Source: "hyperliquid", SourceTier: "MARKET_API",
			Freshness: "REALTIME", Adjustment: "NOT_APPLICABLE",
		})
	}
	return result
}
