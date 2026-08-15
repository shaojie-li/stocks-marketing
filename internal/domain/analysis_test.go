package domain

import (
	"encoding/json"
	"os"
	"testing"
)

func TestComputeIndicatorsReplaysFrozenReport(t *testing.T) {
	t.Parallel()

	report, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	indicators, err := ComputeIndicators(report)
	if err != nil {
		t.Fatalf("ComputeIndicators() error = %v", err)
	}
	want := map[string]string{
		"memory_relative_strength":        "1.30",
		"semiconductor_relative_strength": "0.70",
		"ai_compute_rotation":             "0.30",
		"skhy_sector_alpha":               "2.00",
		"skhy_market_alpha":               "2.20",
	}
	for name, value := range want {
		if indicators[name] != value {
			t.Errorf("%s = %q, want %q", name, indicators[name], value)
		}
	}
}

func TestComputeIndicatorsFailsClosedWhenObservationIsMissing(t *testing.T) {
	t.Parallel()

	report := []byte(`{"observations":[{"symbol":"xyz:MU","change_pct":"1.00"}]}`)
	if _, err := ComputeIndicators(report); err == nil {
		t.Fatal("ComputeIndicators() accepted an incomplete input")
	}
}

func TestComputeIndicatorsRejectsStaleConflictingAndMismatchedInputs(t *testing.T) {
	t.Parallel()

	original, err := os.ReadFile("../../testdata/global-analysis/v1/example-report.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
	}{
		{
			name: "stale",
			mutate: func(observations []map[string]any) []map[string]any {
				observations[0]["freshness"] = "STALE"
				return observations
			},
		},
		{
			name: "duplicate source conflict",
			mutate: func(observations []map[string]any) []map[string]any {
				return append(observations, observations[0])
			},
		},
		{
			name: "mismatched window",
			mutate: func(observations []map[string]any) []map[string]any {
				for _, observation := range observations {
					if observation["symbol"] == "xyz:SMH" {
						observation["window_type"] = "REGULAR_SESSION"
					}
				}
				return observations
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var report map[string]any
			if err := json.Unmarshal(original, &report); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			rawObservations := report["observations"].([]any)
			observations := make([]map[string]any, len(rawObservations))
			for index, observation := range rawObservations {
				observations[index] = observation.(map[string]any)
			}
			mutated := test.mutate(observations)
			report["observations"] = mutated
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if _, err := ComputeIndicators(encoded); err == nil {
				t.Fatal("ComputeIndicators() accepted invalid observations")
			}
		})
	}
}
