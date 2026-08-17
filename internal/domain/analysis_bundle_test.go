package domain

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
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
	if first.Identity.RuleVersion != "global-analysis/1.7.0" || first.Identity.WindowType != "CONTRACT_24H" || first.Identity.WindowStart != "2026-08-14T06:00:00Z" || first.Identity.WindowEnd != first.Identity.AsOfBucket {
		t.Fatalf("stable identity is incomplete: %#v", first.Identity)
	}
	if first.Trend.Value != "9.4" || first.Trend.CoveragePct != 80 || first.Trend.ConfidenceMax != ConfidenceMedium || first.Trend.Direction != "" {
		t.Fatalf("Trend Score = %#v, want first price-only 9.4/80/MEDIUM", first.Trend)
	}
	if first.Unavailable.PriceStructure != AvailabilityUnavailable || first.Unavailable.ForeignFlow != AvailabilityUnavailable || first.Unavailable.Catalyst != AvailabilityUnavailable || first.Unavailable.Crowding != AvailabilityUnavailable {
		t.Fatalf("missing signals were not explicit: %#v", first.Unavailable)
	}
	if first.MemoryDay.Classification != MemoryDayDataUnavailable || first.Memory.State != MemoryTrendWeak || first.Memory.ConfidenceMax != ConfidenceLow {
		t.Fatalf("Memory result = %#v / %#v, want DATA_UNAVAILABLE and unchanged WEAK", first.MemoryDay, first.Memory)
	}
	if first.Quality.DataFreshness != "REALTIME" || first.Quality.DataCompleteness != "MEDIUM" || first.Quality.ConfidenceMax != ConfidenceLow {
		t.Fatalf("quality summary = %#v", first.Quality)
	}
}

func TestBuildAnalysisBundleFreezesCrowdingIntoInputHash(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 6, 0, 0, 0, time.UTC)
	makeCrowding := func(currentFunding string) Crowding {
		return CalculateCrowding(CrowdingInput{
			Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano),
			FundingHistory: crowdingFundingHistory(asOf, currentFunding, "-0.0004"), DailyBars: crowdingDailyBars(asOf, 50),
			CurrentOpenInterest: "100", FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"}, OIEvidenceRefs: []string{"ev-oi"},
		})
	}
	input := AnalysisBundleInput{
		Phase: "LIVE_CHECK", PrimaryAsset: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), AsOfBucket: asOf.Format(time.RFC3339Nano),
		Observations: coreObservations(), Crowding: makeCrowding("0.0004"),
	}
	first, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Crowding.Availability != AvailabilityAvailable || first.Unavailable.Crowding != AvailabilityAvailable {
		t.Fatalf("Crowding was not frozen: %#v", first.Crowding)
	}
	input.Crowding = makeCrowding("0.0005")
	changed, err := BuildAnalysisBundle(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.InputHash == first.InputHash {
		t.Fatal("Crowding input change did not change Analysis Bundle hash")
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
		RuleVersion: "global-analysis/1.7.0", State: MemoryTrendImproving,
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

func TestBuildDegradedShadowReportFreezesRealSafetyVector(t *testing.T) {
	returns := map[string]string{
		"xyz:MU": "0.46048294", "xyz:SMH": "-0.32024529", "xyz:XYZ100": "0.06997201",
		"xyz:AMD": "2.29704779", "xyz:NVDA": "-0.39410176", "xyz:SKHY": "-0.07198560",
		"xyz:SMSN": "-0.92390740", "xyz:KR200": "-0.65490267",
	}
	prices := map[string]string{
		"xyz:MU": "975.19", "xyz:SMH": "585.17", "xyz:XYZ100": "30033.0", "xyz:AMD": "515.26",
		"xyz:NVDA": "224.94", "xyz:SKHY": "166.58", "xyz:SMSN": "190.88", "xyz:KR200": "1092.2",
	}
	observations := coreObservations()
	for index := range observations {
		observation := &observations[index]
		observation.Price = prices[observation.Symbol]
		observation.ChangePct = returns[observation.Symbol]
		observation.ObservedAt = "2026-08-15T15:11:35.036701Z"
		observation.WindowStart = "2026-08-14T15:10:59.999Z"
		observation.WindowEnd = "2026-08-15T15:11:35.036701Z"
	}
	catalyst, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-15T15:11:35.036701Z", CatalystPriceWindow{
		TargetSymbol: "xyz:SKHY", BenchmarkSymbol: "xyz:SMSN",
		WindowStart: "2026-08-14T07:43:59.999Z", WindowEnd: "2026-08-15T07:43:59.999Z",
		TargetStartPrice: "163.93", TargetEndPrice: "166.31", BenchmarkStartPrice: "192.45", BenchmarkEndPrice: "190.40",
		EvidenceRefs: []string{"ev-skhy-price", "ev-smsn-price"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{
		Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: "2026-08-15T15:11:35.036701Z",
		AsOfBucket: "2026-08-15T15:11:00Z", Observations: observations, Catalyst: catalyst,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Trend.Value != "7.2" || bundle.Trend.CoveragePct != 80 || bundle.Catalyst.State != CatalystRejected {
		t.Fatalf("real safety vector = Trend %s/%d, Catalyst %s", bundle.Trend.Value, bundle.Trend.CoveragePct, bundle.Catalyst.State)
	}
	report, err := BuildDegradedShadowReport(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Scores struct {
			Fundamental struct {
				Value any `json:"value"`
			} `json:"fundamental"`
			Entry struct {
				Value any `json:"value"`
			} `json:"entry"`
			Trend struct {
				Value float64 `json:"value"`
			} `json:"trend"`
		} `json:"scores"`
		Catalyst struct {
			ExpectedDirection string `json:"expected_direction"`
			State             string `json:"state"`
		} `json:"catalyst"`
		Strategy struct {
			Current       string `json:"current"`
			BestStructure string `json:"best_structure"`
		} `json:"strategy"`
		DataAudit struct{ Confidence string } `json:"data_audit"`
	}
	if err := json.Unmarshal(report, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scores.Trend.Value != 7.2 || decoded.Scores.Fundamental.Value != nil || decoded.Scores.Entry.Value != nil ||
		decoded.Catalyst.ExpectedDirection != "BEARISH" || decoded.Catalyst.State != "REJECTED" ||
		decoded.Strategy.Current != "OBSERVE" || decoded.Strategy.BestStructure != "NO_TRADE" || decoded.DataAudit.Confidence != "LOW" {
		t.Fatalf("degraded shadow report = %#v", decoded)
	}
}

func TestBuildDegradedShadowReportRejectsInvalidTrendScore(t *testing.T) {
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{
		Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: "2026-08-15T06:00:00Z",
		AsOfBucket: "2026-08-15T06:00:00Z", Observations: coreObservations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle.Trend.Value = "not-a-score"
	if _, err = BuildDegradedShadowReport(bundle); err == nil {
		t.Fatal("invalid Trend Score was silently accepted")
	}
}

func TestBuildDegradedShadowReportUsesFirstNonPassConfirmationStep(t *testing.T) {
	observations := coreObservations()
	for index := range observations {
		if observations[index].Symbol == "xyz:MU" {
			observations[index].ChangePct = "-1.00"
		}
	}
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{
		Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: "2026-08-15T06:00:00Z",
		AsOfBucket: "2026-08-15T06:00:00Z", Observations: observations,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildDegradedShadowReport(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ConfirmationChain struct {
			Status     string `json:"status"`
			FirstBreak int    `json:"first_break"`
		} `json:"confirmation_chain"`
	}
	if err := json.Unmarshal(report, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ConfirmationChain.Status != "BROKEN" || decoded.ConfirmationChain.FirstBreak != 1 {
		t.Fatalf("confirmation chain = %#v", decoded.ConfirmationChain)
	}
}

func TestBuildDegradedShadowReportKeepsSafetyGateWithAvailableCrowding(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 6, 0, 0, 0, time.UTC)
	crowding := CalculateCrowding(CrowdingInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), FundingHistory: crowdingFundingHistory(asOf, "0.0004", "-0.0004"), DailyBars: crowdingDailyBars(asOf, 50),
		CurrentOpenInterest: "100", FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"}, OIEvidenceRefs: []string{"ev-oi"},
	})
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{
		Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), AsOfBucket: asOf.Format(time.RFC3339Nano), Observations: coreObservations(), Crowding: crowding,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildDegradedShadowReport(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Crowding struct {
			Availability    string   `json:"availability"`
			State           string   `json:"state"`
			AvailableInputs int      `json:"available_inputs"`
			EvidenceRefs    []string `json:"evidence_refs"`
		} `json:"crowding"`
		Scores struct {
			Entry struct {
				Value       any     `json:"value"`
				CoveragePct float64 `json:"coverage_pct"`
			} `json:"entry"`
		} `json:"scores"`
		Strategy struct {
			BestStructure string `json:"best_structure"`
		} `json:"strategy"`
		DataAudit struct {
			MissingFields []string `json:"missing_fields"`
		} `json:"data_audit"`
	}
	if err := json.Unmarshal(report, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Crowding.Availability != AvailabilityAvailable || decoded.Crowding.State != string(CrowdingHigh) || decoded.Crowding.AvailableInputs != 4 || len(decoded.Crowding.EvidenceRefs) != 1 {
		t.Fatalf("report Crowding = %#v", decoded.Crowding)
	}
	if decoded.Scores.Entry.Value != nil || decoded.Scores.Entry.CoveragePct != 20 || decoded.Strategy.BestStructure != "NO_TRADE" || slices.Contains(decoded.DataAudit.MissingFields, "crowding") {
		t.Fatalf("available Crowding changed safety gate: %#v", decoded)
	}
}

func TestBuildDegradedShadowReportKeepsSafetyGateWithAvailableFundamental(t *testing.T) {
	asOf := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	fundamental := CalculateFundamental(validFundamentalInput())
	bundle, err := BuildAnalysisBundle(AnalysisBundleInput{Phase: "GLOBAL", PrimaryAsset: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), AsOfBucket: asOf.Format(time.RFC3339Nano), Observations: coreObservations(), Fundamental: fundamental})
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildDegradedShadowReport(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Scores struct {
			Fundamental struct {
				Value       any     `json:"value"`
				CoveragePct float64 `json:"coverage_pct"`
			} `json:"fundamental"`
			Entry struct {
				Value any `json:"value"`
			} `json:"entry"`
		} `json:"scores"`
		Strategy struct {
			BestStructure string `json:"best_structure"`
		} `json:"strategy"`
		DataAudit struct {
			MissingFields []string `json:"missing_fields"`
		} `json:"data_audit"`
	}
	if json.Unmarshal(report, &decoded) != nil || decoded.Scores.Fundamental.Value == nil || decoded.Scores.Fundamental.CoveragePct != 90 || decoded.Scores.Entry.Value != nil || decoded.Strategy.BestStructure != "NO_TRADE" || slices.Contains(decoded.DataAudit.MissingFields, "scores.fundamental") {
		t.Fatalf("available Fundamental changed safety gate: %#v", decoded)
	}
}
