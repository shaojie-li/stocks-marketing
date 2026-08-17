package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSubmitRejectsDegradedReportBeforeDeliverySideEffects(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
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
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	_, err = (&App{}).Submit(context.Background(), encoded)
	if err == nil || !strings.Contains(err.Error(), "shadow-only") {
		t.Fatalf("Submit() error = %v", err)
	}
}

func TestSubmitRejectsIncompleteConfirmationBeforeDeliverySideEffects(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	chain := report["confirmation_chain"].(map[string]any)
	chain["status"] = "INCOMPLETE"
	chain["first_break"] = float64(4)
	step := chain["steps"].([]any)[3].(map[string]any)
	step["status"] = "UNAVAILABLE"
	step["evidence_refs"] = []any{}
	strategy := report["strategy"].(map[string]any)
	strategy["current"] = "OBSERVE"
	strategy["best_structure"] = "NO_TRADE"
	strategy["rationale"] = "确认链不完整，仅保留观察结论。"
	strategy["invalidation_conditions"] = []any{}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	_, err = (&App{}).Submit(context.Background(), encoded)
	if err == nil || !strings.Contains(err.Error(), "CONFIRMATION_CHAIN_INCOMPLETE") {
		t.Fatalf("Submit() error = %v", err)
	}
}
