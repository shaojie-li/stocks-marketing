package domain

import (
	"encoding/json"
	"os"
	"testing"
)

func TestClassifyMemoryDayUsesEvidenceAndFailsClosed(t *testing.T) {
	tests := map[string]struct {
		mutate func(*MemoryDayInput)
		want   MemoryDayClassification
	}{
		"supportive": {func(*MemoryDayInput) {}, MemoryDaySupportive},
		"hard failure": {func(input *MemoryDayInput) {
			input.Indicators.CrossMarket.State = CrossMarketDivergence
			input.Indicators.CrossMarket.Direction = DirectionNone
			input.ForeignFlow = ForeignFlowNetSell
			input.Catalyst = CatalystRejected
		}, MemoryDayHardFailure},
		"hard failure on broken structure": {func(input *MemoryDayInput) {
			input.Indicators.CrossMarket.State = CrossMarketDivergence
			input.Indicators.CrossMarket.Direction = DirectionNone
			input.ForeignFlow = ForeignFlowNetSell
			input.Catalyst = CatalystNeutral
			input.PriceStructure = PriceStructureBroken
		}, MemoryDayHardFailure},
		"adverse": {func(input *MemoryDayInput) {
			for _, name := range []string{IndicatorMemoryRelativeStrength, IndicatorSemiconductorRelativeStrength, IndicatorSKHYSectorAlpha, IndicatorSKHYMarketAlpha} {
				indicator := input.Indicators.Relative[name]
				indicator.State = RelativeWeak
				input.Indicators.Relative[name] = indicator
			}
			input.Indicators.CrossMarket.State = CrossMarketConfirmed
			input.Indicators.CrossMarket.Direction = DirectionBearish
			input.ForeignFlow = ForeignFlowNetSell
			input.Catalyst = CatalystNeutral
			input.PriceStructure = PriceStructureRange
		}, MemoryDayAdverse},
		"neutral": {func(input *MemoryDayInput) {
			for _, name := range []string{IndicatorMemoryRelativeStrength, IndicatorSemiconductorRelativeStrength, IndicatorSKHYSectorAlpha, IndicatorSKHYMarketAlpha} {
				indicator := input.Indicators.Relative[name]
				indicator.State = RelativeNeutral
				input.Indicators.Relative[name] = indicator
			}
			input.Indicators.CrossMarket.State = CrossMarketNeutral
			input.Indicators.CrossMarket.Direction = DirectionNone
			input.ForeignFlow = ForeignFlowNeutral
			input.Catalyst = CatalystNeutral
			input.PriceStructure = PriceStructureRange
		}, MemoryDayNeutral},
		"unavailable foreign flow": {func(input *MemoryDayInput) { input.ForeignFlow = "" }, MemoryDayDataUnavailable},
		"data conflict":            {func(input *MemoryDayInput) { input.DataConflict = true }, MemoryDayDataUnavailable},
		"market closed": {func(input *MemoryDayInput) {
			input.MarketClosed = true
			input.ForeignFlow = ""
		}, MemoryDayMarketClosed},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := MemoryDayInput{
				Indicators: scoredCoreIndicators(t), ForeignFlow: ForeignFlowNetBuy,
				Catalyst: CatalystAccepted, PriceStructure: PriceStructureAboveSupport,
				ForeignFlowRefs: []string{"ev-foreign-flow"}, CatalystRefs: []string{"ev-catalyst"},
				PriceStructureRefs: []string{"ev-price-structure"},
			}
			test.mutate(&input)
			got, err := ClassifyMemoryDay(input)
			if err != nil || got.Classification != test.want {
				t.Fatalf("classification = %#v, %v; want %s", got, err, test.want)
			}
		})
	}
}

func TestClassifyMemoryDayRejectsAvailableEvidenceWithoutReferences(t *testing.T) {
	_, err := ClassifyMemoryDay(MemoryDayInput{
		Indicators: scoredCoreIndicators(t), ForeignFlow: ForeignFlowNetBuy,
		Catalyst: CatalystAccepted, PriceStructure: PriceStructureAboveSupport,
	})
	if err == nil {
		t.Fatal("available Memory evidence without references was accepted")
	}
}

func TestAdvanceMemoryTrendReplaysFrozenTransitions(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/global-analysis/v1/memory-state-transitions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string `json:"name"`
			Input struct {
				State                  MemoryTrendState        `json:"state"`
				Day                    MemoryDayClassification `json:"day"`
				SupportiveStreakBefore int                     `json:"supportive_streak_before"`
				AdverseStreakBefore    int                     `json:"adverse_streak_before"`
			} `json:"input"`
			Expected struct {
				State                 MemoryTrendState `json:"state"`
				SupportiveStreakAfter int              `json:"supportive_streak_after"`
				AdverseStreakAfter    int              `json:"adverse_streak_after"`
				Transitioned          bool             `json:"transitioned"`
				ConfidenceMax         Confidence       `json:"confidence_max"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			evidenceRefs := []string{"ev-memory-day"}
			if test.Input.Day == MemoryDayMarketClosed || test.Input.Day == MemoryDayDataUnavailable {
				evidenceRefs = nil
			}
			got, err := AdvanceMemoryTrend(MemoryTransitionInput{
				State: test.Input.State, Day: test.Input.Day,
				SupportiveStreak: test.Input.SupportiveStreakBefore,
				AdverseStreak:    test.Input.AdverseStreakBefore,
				LastSessionDate:  "2026-08-14", SessionDate: "2026-08-15",
				EvidenceRefs: evidenceRefs,
			})
			if err != nil {
				t.Fatalf("advance Memory trend: %v", err)
			}
			if got.State != test.Expected.State || got.Transitioned != test.Expected.Transitioned || got.SupportiveStreak != test.Expected.SupportiveStreakAfter || got.AdverseStreak != test.Expected.AdverseStreakAfter {
				t.Fatalf("transition = %#v, want %#v", got, test.Expected)
			}
			if test.Expected.ConfidenceMax != "" && got.ConfidenceMax != test.Expected.ConfidenceMax {
				t.Fatalf("confidence max = %s, want %s", got.ConfidenceMax, test.Expected.ConfidenceMax)
			}
			if got.RuleVersion != "global-analysis/1.4.0" || got.Reason == "" {
				t.Fatalf("transition audit is incomplete: %#v", got)
			}
		})
	}
}

func TestAdvanceMemoryTrendRejectsInvalidOrOutOfOrderState(t *testing.T) {
	tests := map[string]MemoryTransitionInput{
		"invalid state":    {State: "UNKNOWN", Day: MemoryDayNeutral, LastSessionDate: "2026-08-14", SessionDate: "2026-08-15"},
		"negative streak":  {State: MemoryTrendWeak, Day: MemoryDayNeutral, SupportiveStreak: -1, LastSessionDate: "2026-08-14", SessionDate: "2026-08-15"},
		"duplicate date":   {State: MemoryTrendWeak, Day: MemoryDayNeutral, LastSessionDate: "2026-08-15", SessionDate: "2026-08-15"},
		"earlier date":     {State: MemoryTrendWeak, Day: MemoryDayNeutral, LastSessionDate: "2026-08-16", SessionDate: "2026-08-15"},
		"missing evidence": {State: MemoryTrendWeak, Day: MemoryDaySupportive, LastSessionDate: "2026-08-14", SessionDate: "2026-08-15"},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := AdvanceMemoryTrend(input); err == nil {
				t.Fatal("invalid Memory transition was accepted")
			}
		})
	}
}
