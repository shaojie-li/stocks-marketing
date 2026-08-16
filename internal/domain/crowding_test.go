package domain

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalculateCrowdingUsesFourRealInputsWithoutInventingOI(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	result := CalculateCrowding(CrowdingInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano),
		FundingHistory:      crowdingFundingHistory(asOf, "0.0004", "-0.0004"),
		DailyBars:           crowdingDailyBars(asOf, 50),
		CurrentOpenInterest: "1397451.7",
		FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"},
		OIEvidenceRefs: []string{"ev-current-oi"},
	})

	if result.Availability != AvailabilityAvailable || result.State != CrowdingHigh || result.Direction != CrowdingLong {
		t.Fatalf("Crowding = %#v, want AVAILABLE/HIGH/LONG", result)
	}
	if result.AvailableInputs != 4 || result.TriggerCount != 4 {
		t.Fatalf("coverage = %d inputs/%d triggers, want 4/4", result.AvailableInputs, result.TriggerCount)
	}
	oi := result.Component(CrowdingInputOpenInterest)
	if oi.Availability != AvailabilityUnavailable || oi.Reason != CrowdingReasonHistoricalOIUnavailable || oi.CurrentValue != "1397451.70000000" || oi.Percentile != "" {
		t.Fatalf("OI component invented unavailable history: %#v", oi)
	}
	if result.Component(CrowdingInputFunding).Percentile != "100.00000000" || result.Component(CrowdingInputPremium).Direction != CrowdingShort {
		t.Fatalf("funding/premium evidence was not preserved: %#v", result.Components)
	}
}

func TestCalculateCrowdingKeepsCurrentShortHistoryUnavailable(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	result := CalculateCrowding(CrowdingInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano),
		FundingHistory: crowdingFundingHistory(asOf, "0.0004", "-0.0004"),
		DailyBars:      crowdingDailyBars(asOf, 38), FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"},
	})

	if result.Availability != AvailabilityUnavailable || result.Reason != CrowdingReasonInsufficientInputs || result.State != "" || result.Direction != "" {
		t.Fatalf("short live history produced a state: %#v", result)
	}
	if result.AvailableInputs != 3 || result.Component(CrowdingInputPriceExtension).Reason != CrowdingReasonInsufficientHistory || result.Component(CrowdingInputVolume).Availability != AvailabilityAvailable {
		t.Fatalf("partial coverage was not explicit: %#v", result.Components)
	}
}

func TestCalculateCrowdingRejectsFundingGapAndStaleLatestHour(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		history []CrowdingFundingSample
		reason  string
	}{
		{name: "gap", history: append(crowdingFundingHistory(asOf, "0.0004", "0.0004")[:400], crowdingFundingHistory(asOf, "0.0004", "0.0004")[401:]...), reason: CrowdingReasonInvalidHistory},
		{name: "stale", history: crowdingFundingHistory(asOf.Add(-2*time.Hour), "0.0004", "0.0004"), reason: CrowdingReasonStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := CalculateCrowding(CrowdingInput{
				Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), FundingHistory: test.history,
				DailyBars: crowdingDailyBars(asOf, 50), FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"},
			})
			for _, name := range []string{CrowdingInputFunding, CrowdingInputPremium} {
				component := result.Component(name)
				if component.Availability != AvailabilityUnavailable && component.Availability != AvailabilityStale {
					t.Fatalf("%s remained available: %#v", name, component)
				}
				if component.Reason != test.reason {
					t.Fatalf("%s reason = %s, want %s", name, component.Reason, test.reason)
				}
			}
		})
	}
}

func TestCalculateCrowdingMarksOldDailyHistoryStale(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	result := CalculateCrowding(CrowdingInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), FundingHistory: crowdingFundingHistory(asOf, "0.0004", "0.0004"),
		DailyBars: crowdingDailyBars(asOf.Add(-48*time.Hour), 50), FundingEvidenceRefs: []string{"ev-funding"}, DailyEvidenceRefs: []string{"ev-daily"},
	})
	for _, name := range []string{CrowdingInputPriceExtension, CrowdingInputVolume} {
		component := result.Component(name)
		if component.Availability != AvailabilityStale || component.Reason != CrowdingReasonStale {
			t.Fatalf("%s = %#v, want STALE", name, component)
		}
	}
}

func TestEmpiricalPercentileAndCrowdingThresholdsIncludeEquality(t *testing.T) {
	values := make([]string, 20)
	for index := range values {
		if index < 15 {
			values[index] = "1"
		} else {
			values[index] = "2"
		}
	}
	percentile, err := empiricalPercentile(values, "1", false)
	if err != nil || percentile != "75.00000000" {
		t.Fatalf("percentile = %s, %v; want exact 75", percentile, err)
	}
	if state := crowdingState(4, "2.50000000"); state != CrowdingExtreme {
		t.Fatalf("4 triggers at 2.5 ATR = %s, want EXTREME", state)
	}
	if state := crowdingState(5, "0"); state != CrowdingExtreme {
		t.Fatalf("5 triggers = %s, want EXTREME", state)
	}
}

func TestCrowdingStateBoundariesMatchFrozenVectors(t *testing.T) {
	var fixture struct {
		RuleVersion string `json:"rule_version"`
		Cases       []struct {
			Name          string        `json:"name"`
			TriggerCount  int           `json:"trigger_count"`
			ExtensionATR  string        `json:"extension_atr"`
			ExpectedState CrowdingState `json:"expected_state"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "global-analysis", "v1", "crowding-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || fixture.RuleVersion != GlobalAnalysisRuleVersion {
		t.Fatalf("invalid Crowding boundary fixture: %v", err)
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			if got := crowdingState(test.TriggerCount, test.ExtensionATR); got != test.ExpectedState {
				t.Fatalf("state = %s, want %s", got, test.ExpectedState)
			}
		})
	}
}

func crowdingFundingHistory(asOf time.Time, currentFunding, currentPremium string) []CrowdingFundingSample {
	latest := asOf.UTC().Truncate(time.Hour)
	if !latest.Before(asOf) {
		latest = latest.Add(-time.Hour)
	}
	start := latest.Add(-719 * time.Hour)
	result := make([]CrowdingFundingSample, 720)
	for index := range result {
		result[index] = CrowdingFundingSample{
			Symbol: "xyz:SKHY", Time: start.Add(time.Duration(index) * time.Hour).Add(100 * time.Millisecond).Format(time.RFC3339Nano),
			FundingRate: "0.0001", Premium: "0.0001",
		}
	}
	result[len(result)-1].FundingRate = currentFunding
	result[len(result)-1].Premium = currentPremium
	return result
}

func crowdingDailyBars(asOf time.Time, completed int) []DailyPriceBar {
	start := asOf.UTC().Truncate(24*time.Hour).AddDate(0, 0, -completed)
	result := make([]DailyPriceBar, completed+1)
	for index := range result {
		openTime := start.AddDate(0, 0, index)
		price := 100 + index
		volume := "100"
		if index == completed-1 {
			volume = "200"
		}
		result[index] = DailyPriceBar{
			Symbol: "xyz:SKHY", Interval: "1d", OpenTime: openTime.Format(time.RFC3339Nano), CloseTime: openTime.Add(24*time.Hour - time.Millisecond).Format(time.RFC3339Nano),
			Open: decimalInteger(price), Close: decimalInteger(price), High: decimalInteger(price + 2), Low: decimalInteger(price - 2), Volume: volume,
		}
	}
	return result
}

func decimalInteger(value int) string {
	return new(big.Int).SetInt64(int64(value)).String()
}
