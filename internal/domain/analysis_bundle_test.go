package domain

import (
	"slices"
	"testing"
)

func TestBuildAnalysisBundleIsDeterministicAndKeepsUnavailableSignalsExplicit(t *testing.T) {
	input := AnalysisBundleInput{
		Phase:        "LIVE_CHECK",
		PrimaryAsset: "xyz:SKHY",
		AsOf:         "2026-08-15T06:00:00Z",
		AsOfBucket:   "2026-08-15T06:00:00Z",
		Observations: coreObservations(),
	}
	first, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatalf("build Analysis Bundle: %v", err)
	}
	reversed := slices.Clone(input.Observations)
	slices.Reverse(reversed)
	input.Observations = reversed
	second, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatalf("build reordered Analysis Bundle: %v", err)
	}
	if first.InputHash != second.InputHash {
		t.Fatalf("input order changed hash: %s != %s", first.InputHash, second.InputHash)
	}
	offset := input
	offset.AsOf = "2026-08-15T14:00:00+08:00"
	offset.AsOfBucket = "2026-08-15T14:00:00+08:00"
	offset.Observations = slices.Clone(input.Observations)
	for index := range offset.Observations {
		offset.Observations[index].ObservedAt = "2026-08-15T14:00:00+08:00"
		offset.Observations[index].WindowStart = "2026-08-14T14:00:00+08:00"
		offset.Observations[index].WindowEnd = "2026-08-15T14:00:00+08:00"
	}
	third, err := BuildAnalysisBundle(offset)
	if err != nil {
		t.Fatalf("build offset Analysis Bundle: %v", err)
	}
	if third.InputHash != first.InputHash {
		t.Fatalf("same instants changed hash: %s != %s", third.InputHash, first.InputHash)
	}
	if third.Memory.SessionDate != "2026-08-15" {
		t.Fatalf("civil session date shifted across timezone: %s", third.Memory.SessionDate)
	}
	if first.Identity.RuleVersion != "global-analysis/1.1.0" || first.Identity.WindowType != "CONTRACT_24H" || first.Identity.WindowStart != "2026-08-14T06:00:00Z" || first.Identity.WindowEnd != first.Identity.AsOfBucket {
		t.Fatalf("stable identity is incomplete: %#v", first.Identity)
	}
	if first.Trend.Value != "9.4" || first.Trend.CoveragePct != 80 || first.Trend.ConfidenceMax != ConfidenceMedium || first.Trend.Direction != "" {
		t.Fatalf("Trend Score = %#v, want first price-only 9.4/80/MEDIUM", first.Trend)
	}
	if first.Unavailable.PriceStructure != AvailabilityUnavailable || first.Unavailable.ForeignFlow != AvailabilityUnavailable {
		t.Fatalf("missing signals were not explicit: %#v", first.Unavailable)
	}
	if first.MemoryDay.Classification != MemoryDayDataUnavailable || first.Memory.State != MemoryTrendWeak || first.Memory.ConfidenceMax != ConfidenceLow {
		t.Fatalf("Memory result = %#v / %#v, want DATA_UNAVAILABLE and unchanged WEAK", first.MemoryDay, first.Memory)
	}
	if first.Quality.DataFreshness != "REALTIME" || first.Quality.DataCompleteness != "MEDIUM" || first.Quality.ConfidenceMax != ConfidenceLow {
		t.Fatalf("quality summary = %#v", first.Quality)
	}
}

func TestBuildAnalysisBundleFailsClosedWhenObservationIsAfterAsOf(t *testing.T) {
	input := AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY",
		AsOf: "2026-08-15T05:59:59Z", AsOfBucket: "2026-08-15T06:00:00Z",
		Observations: coreObservations(),
	}
	if _, err := BuildAnalysisBundle(input); err == nil {
		t.Fatal("observation after as_of was accepted")
	}
}

func TestBuildAnalysisBundleUsesComparableScoreAndPreviousMemory(t *testing.T) {
	previousTrend, err := CalculateTrendScore(TrendScoreInput{Indicators: scoredCoreIndicators(t), Phase: "LIVE_CHECK"})
	if err != nil {
		t.Fatal(err)
	}
	previousTrend.Value = "8.9"
	previousMemory := &MemoryTrendTransition{
		RuleVersion: "global-analysis/1.1.0", State: MemoryTrendImproving,
		SupportiveStreak: 1, AdverseStreak: 1, SessionDate: "2026-08-14",
	}
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY",
		AsOf: "2026-08-15T06:00:00Z", AsOfBucket: "2026-08-15T06:00:00Z",
		Observations: coreObservations(), PreviousTrend: &previousTrend, PreviousMemory: previousMemory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Trend.Direction != ScoreDirectionUp {
		t.Fatalf("Trend direction = %s, want UP", bundle.Trend.Direction)
	}
	if bundle.Memory.PreviousState != MemoryTrendImproving || bundle.Memory.State != MemoryTrendImproving || bundle.Memory.SupportiveStreak != 0 || bundle.Memory.AdverseStreak != 0 {
		t.Fatalf("Memory did not use persisted previous state: %#v", bundle.Memory)
	}
}
