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
	if first.Identity.RuleVersion != "global-analysis/1.3.0" || first.Identity.WindowType != "CONTRACT_24H" || first.Identity.WindowStart != "2026-08-14T06:00:00Z" || first.Identity.WindowEnd != first.Identity.AsOfBucket {
		t.Fatalf("stable identity is incomplete: %#v", first.Identity)
	}
	if first.Trend.Value != "9.4" || first.Trend.CoveragePct != 80 || first.Trend.ConfidenceMax != ConfidenceMedium || first.Trend.Direction != "" {
		t.Fatalf("Trend Score = %#v, want first price-only 9.4/80/MEDIUM", first.Trend)
	}
	if first.Unavailable.PriceStructure != AvailabilityUnavailable || first.Unavailable.ForeignFlow != AvailabilityUnavailable || first.Unavailable.Catalyst != AvailabilityUnavailable {
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
		RuleVersion: "global-analysis/1.3.0", State: MemoryTrendImproving,
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

func TestBuildAnalysisBundleFreezesAvailablePriceStructure(t *testing.T) {
	input := AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY",
		AsOf: "2026-08-15T06:00:00Z", AsOfBucket: "2026-08-15T06:00:00Z",
		Observations: coreObservations(),
		PriceStructure: PriceStructure{
			Availability: AvailabilityAvailable, Symbol: "xyz:SKHY", Interval: "1d", CompletedBars: 50,
			WindowStart: "2026-06-26T00:00:00Z", WindowEnd: "2026-08-14T23:59:59.999Z",
			Close: "100.00000000", EMA20: "99.00000000", EMA50: "98.00000000", ATR14: "4.00000000",
			SupportLow20: "95.00000000", State: PriceStructureAboveSupport, EvidenceRefs: []string{"ev-skhy-daily"},
		},
	}
	available, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatal(err)
	}
	if available.PriceStructure.State != PriceStructureAboveSupport || available.Unavailable.PriceStructure != AvailabilityAvailable || available.Trend.CoveragePct != 90 || available.Trend.ConfidenceMax != ConfidenceMedium {
		t.Fatalf("available Price Structure was not frozen into Bundle: %#v", available)
	}
	if available.MemoryDay.Classification != MemoryDayDataUnavailable || available.Memory.ConfidenceMax != ConfidenceLow {
		t.Fatalf("Price Structure incorrectly filled Foreign Flow or Catalyst: %#v / %#v", available.MemoryDay, available.Memory)
	}
	equivalent := input
	equivalent.PriceStructure.WindowStart = "2026-06-26T08:00:00+08:00"
	equivalent.PriceStructure.WindowEnd = "2026-08-15T07:59:59.999+08:00"
	equivalent.PriceStructure.Close = "100.0"
	sameInstant, err := BuildAnalysisBundle(equivalent)
	if err != nil {
		t.Fatal(err)
	}
	if sameInstant.InputHash != available.InputHash {
		t.Fatal("Price Structure timestamp offset or decimal representation changed Bundle hash")
	}

	input.PriceStructure.State = PriceStructureRange
	changed, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.InputHash == available.InputHash {
		t.Fatal("Price Structure change did not change Bundle input hash")
	}
}

func TestBuildAnalysisBundleFreezesCatalystIntoInputHash(t *testing.T) {
	catalyst, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-15T08:44:00Z", finalCatalystWindow())
	if err != nil {
		t.Fatal(err)
	}
	input := AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY",
		AsOf: "2026-08-15T08:44:00Z", AsOfBucket: "2026-08-15T08:44:00Z",
		Observations: coreObservations(), Catalyst: catalyst,
	}
	for index := range input.Observations {
		input.Observations[index].ObservedAt = input.AsOf
		input.Observations[index].WindowEnd = input.AsOf
	}
	first, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Catalyst.State != CatalystAccepted || first.Unavailable.Catalyst != AvailabilityAvailable {
		t.Fatalf("Catalyst was not frozen: %#v", first)
	}
	if first.MemoryDay.Classification != MemoryDayDataUnavailable {
		t.Fatalf("Catalyst incorrectly filled Foreign Flow: %#v", first.MemoryDay)
	}
	input.Catalyst.State = CatalystNeutral
	changed, err := BuildAnalysisBundle(input)
	if err == nil || changed.InputHash != "" {
		t.Fatal("internally inconsistent Catalyst was accepted")
	}
}
