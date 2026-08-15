package domain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCalculatePriceStructureUsesFrozenEMAAndATRDefinitions(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bars := flatDailyBars(50, asOf.Truncate(24*time.Hour).AddDate(0, 0, -50), "2", "3", "1")
	for index := range bars {
		closeValue := fmt.Sprintf("%d", index+2)
		bars[index].Open = closeValue
		bars[index].Close = closeValue
		bars[index].High = fmt.Sprintf("%d", index+3)
		bars[index].Low = fmt.Sprintf("%d", index+1)
	}
	result := CalculatePriceStructure(PriceStructureInput{Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), Bars: bars, EvidenceRefs: []string{"ev-skhy-daily"}})
	if result.EMA20 != "41.50000000" || result.EMA50 != "26.50000000" || result.ATR14 != "2.00000000" || result.SupportLow20 != "30.00000000" {
		t.Fatalf("frozen EMA/ATR/support definitions changed: %#v", result)
	}
}

func TestPriceStructureStateBoundariesAreExact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "global-analysis", "v1", "price-structure-boundaries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		RuleVersion string `json:"rule_version"`
		Cases       []struct {
			Name         string              `json:"name"`
			Close        string              `json:"close"`
			EMA20        string              `json:"ema20"`
			EMA50        string              `json:"ema50"`
			ATR14        string              `json:"atr14"`
			SupportLow20 string              `json:"support_low_20"`
			Expected     PriceStructureState `json:"expected_state"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || fixture.RuleVersion != GlobalAnalysisRuleVersion {
		t.Fatalf("decode Price Structure boundaries: %v / %s", err, fixture.RuleVersion)
	}
	decimal := func(value string) *big.Rat {
		result, ok := new(big.Rat).SetString(value)
		if !ok {
			t.Fatal("invalid test decimal")
		}
		return result
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			got := classifyPriceStructure(decimal(test.Close), decimal(test.EMA20), decimal(test.EMA50), decimal(test.ATR14), decimal(test.SupportLow20))
			if got != test.Expected {
				t.Fatalf("state = %s, want %s", got, test.Expected)
			}
		})
	}
}

func TestCalculatePriceStructureUsesCompletedContractDays(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bars := flatDailyBars(50, asOf.Truncate(24*time.Hour).AddDate(0, 0, -50), "100", "102", "98")
	bars = append(bars, DailyPriceBar{
		Symbol: "xyz:SKHY", Interval: "1d", OpenTime: asOf.Truncate(24 * time.Hour).Format(time.RFC3339Nano),
		CloseTime: asOf.Truncate(24 * time.Hour).Add(24*time.Hour - time.Millisecond).Format(time.RFC3339Nano),
		Open:      "100", Close: "999", High: "999", Low: "99", Volume: "1",
	})

	result := CalculatePriceStructure(PriceStructureInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), Bars: bars,
		EvidenceRefs: []string{"ev-skhy-daily"},
	})

	if result.Availability != AvailabilityAvailable || result.CompletedBars != 50 || result.State != PriceStructureAboveSupport {
		t.Fatalf("Price Structure = %#v, want 50 completed bars ABOVE_SUPPORT", result)
	}
	if result.Close != "100.00000000" || result.EMA20 != "100.00000000" || result.EMA50 != "100.00000000" || result.ATR14 != "4.00000000" || result.SupportLow20 != "98.00000000" {
		t.Fatalf("flat Price Structure values = %#v", result)
	}
	if result.WindowEnd != bars[49].CloseTime || len(result.EvidenceRefs) != 1 {
		t.Fatalf("Price Structure audit fields = %#v", result)
	}
}

func TestCalculatePriceStructureRequiresFiftyCompletedDays(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	result := CalculatePriceStructure(PriceStructureInput{
		Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano),
		Bars:         flatDailyBars(49, asOf.Truncate(24*time.Hour).AddDate(0, 0, -49), "100", "102", "98"),
		EvidenceRefs: []string{"ev-skhy-daily"},
	})
	if result.Availability != AvailabilityUnavailable || result.CompletedBars != 49 || result.Reason != PriceStructureReasonInsufficientHistory || result.State != "" {
		t.Fatalf("49 completed bars were not explicitly unavailable: %#v", result)
	}
}

func TestCalculatePriceStructureClassifiesRangeAndBrokenAgainstConfirmedPriorSupport(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		lastClose string
		lastLow   string
		want      PriceStructureState
	}{
		{name: "range below EMA but above support", lastClose: "99", lastLow: "98.5", want: PriceStructureRange},
		{name: "broken below confirmed prior support", lastClose: "90", lastLow: "89", want: PriceStructureBroken},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bars := flatDailyBars(50, asOf.Truncate(24*time.Hour).AddDate(0, 0, -50), "100", "102", "98")
			bars[49].Close = test.lastClose
			bars[49].Low = test.lastLow
			result := CalculatePriceStructure(PriceStructureInput{
				Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), Bars: bars,
				EvidenceRefs: []string{"ev-skhy-daily"},
			})
			if result.Availability != AvailabilityAvailable || result.State != test.want {
				t.Fatalf("Price Structure = %#v, want %s", result, test.want)
			}
		})
	}
}

func TestCalculatePriceStructureRejectsNonContinuousDailyEvidence(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	bars := flatDailyBars(50, asOf.Truncate(24*time.Hour).AddDate(0, 0, -50), "100", "102", "98")
	bars[25].OpenTime = time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	result := CalculatePriceStructure(PriceStructureInput{Symbol: "xyz:SKHY", AsOf: asOf.Format(time.RFC3339Nano), Bars: bars, EvidenceRefs: []string{"ev-skhy-daily"}})
	if result.Availability != AvailabilityUnavailable || result.Reason != PriceStructureReasonInvalidHistory {
		t.Fatalf("invalid daily history was accepted: %#v", result)
	}
}

func flatDailyBars(count int, start time.Time, closeValue, highValue, lowValue string) []DailyPriceBar {
	bars := make([]DailyPriceBar, count)
	for index := range bars {
		openTime := start.AddDate(0, 0, index).UTC()
		bars[index] = DailyPriceBar{
			Symbol: "xyz:SKHY", Interval: "1d",
			OpenTime: openTime.Format(time.RFC3339Nano), CloseTime: openTime.Add(24*time.Hour - time.Millisecond).Format(time.RFC3339Nano),
			Open: closeValue, Close: closeValue, High: highValue, Low: lowValue, Volume: fmt.Sprintf("%d", index+1),
		}
	}
	return bars
}
