package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestEvaluateReportSafetyKeepsDegradedAnalysisShadowOnly(t *testing.T) {
	report := loadSafetyReport(t)
	entry := report["scores"].(map[string]any)["entry"].(map[string]any)
	entry["value"] = nil
	entry["direction"] = "UNAVAILABLE"
	entry["coverage_pct"] = float64(20)
	entry["confidence"] = "LOW"
	strategy := report["strategy"].(map[string]any)
	strategy["current"] = "OBSERVE"
	strategy["best_structure"] = "NO_TRADE"
	strategy["rationale"] = "Entry 覆盖不足，仅保留观察结论。"
	strategy["invalidation_conditions"] = []any{}

	decision, err := EvaluateReportSafety(encodeSafetyReport(t, report))
	if err != nil {
		t.Fatalf("EvaluateReportSafety() error = %v", err)
	}
	if decision.FormalDeliveryAllowed || decision.Action != "NO_ENTRY" || decision.Route != "SHADOW_ONLY" {
		t.Fatalf("degraded decision = %#v", decision)
	}
}

func TestEvaluateReportSafetyRejectsDegradedTradeStructureAndLanguage(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"trade structure": func(report map[string]any) {
			report["strategy"].(map[string]any)["best_structure"] = "PULLBACK_LONG"
		},
		"actionable language": func(report map[string]any) {
			report["strategy"].(map[string]any)["rationale"] = "建议买入并设置止损。"
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := loadSafetyReport(t)
			entry := report["scores"].(map[string]any)["entry"].(map[string]any)
			entry["value"] = nil
			entry["coverage_pct"] = float64(20)
			strategy := report["strategy"].(map[string]any)
			strategy["current"] = "OBSERVE"
			strategy["best_structure"] = "NO_TRADE"
			strategy["rationale"] = "仅观察。"
			strategy["invalidation_conditions"] = []any{}
			mutate(report)
			if _, err := EvaluateReportSafety(encodeSafetyReport(t, report)); err == nil {
				t.Fatal("unsafe degraded report was accepted")
			}
		})
	}
}

func TestEvaluateReportSafetyRequiresInvalidationForTradeStructure(t *testing.T) {
	report := loadSafetyReport(t)
	report["strategy"].(map[string]any)["invalidation_conditions"] = []any{}
	if _, err := EvaluateReportSafety(encodeSafetyReport(t, report)); err == nil || !strings.Contains(err.Error(), "invalidation") {
		t.Fatalf("missing invalidation error = %v", err)
	}
}

func TestCatalystEntryValueRespectsEventAndTradeDirections(t *testing.T) {
	tests := []struct {
		name  string
		event MarketDirection
		state CatalystState
		trade TradeDirection
		want  string
	}{
		{"bearish rejected supports long", DirectionBearish, CatalystRejected, TradeLong, "2.0"},
		{"bearish rejected opposes short", DirectionBearish, CatalystRejected, TradeShort, "0.0"},
		{"bearish accepted supports short", DirectionBearish, CatalystAccepted, TradeShort, "2.0"},
		{"bullish accepted supports long", DirectionBullish, CatalystAccepted, TradeLong, "2.0"},
		{"neutral is neutral", DirectionBearish, CatalystNeutral, TradeLong, "1.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CatalystEntryValue(test.event, test.state, test.trade)
			if err != nil || got != test.want {
				t.Fatalf("CatalystEntryValue() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func loadSafetyReport(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func encodeSafetyReport(t *testing.T, report map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
