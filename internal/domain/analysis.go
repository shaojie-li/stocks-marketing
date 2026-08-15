package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type Observation struct {
	ID           string `json:"observation_id"`
	Symbol       string `json:"symbol"`
	Price        string `json:"price"`
	ChangePct    string `json:"change_pct"`
	ObservedAt   string `json:"observed_at"`
	MarketStatus string `json:"market_status"`
	WindowType   string `json:"window_type"`
	SessionDate  string `json:"session_date"`
	Source       string `json:"source"`
	Freshness    string `json:"freshness"`
}

type FeatureSnapshot map[string]string

type reportInput struct {
	Observations []Observation `json:"observations"`
}

var indicatorPairs = map[string][2]string{
	"memory_relative_strength":        {"xyz:MU", "xyz:SMH"},
	"semiconductor_relative_strength": {"xyz:SMH", "xyz:XYZ100"},
	"ai_compute_rotation":             {"xyz:AMD", "xyz:NVDA"},
	"skhy_sector_alpha":               {"xyz:SKHY", "xyz:SMSN"},
	"skhy_market_alpha":               {"xyz:SKHY", "xyz:KR200"},
}

func ComputeIndicators(report []byte) (FeatureSnapshot, error) {
	var input reportInput
	if err := json.Unmarshal(report, &input); err != nil {
		return nil, fmt.Errorf("decode analysis input: %w", err)
	}
	returns := make(map[string]*big.Rat, len(input.Observations))
	observations := make(map[string]Observation, len(input.Observations))
	for _, observation := range input.Observations {
		if _, exists := returns[observation.Symbol]; exists {
			return nil, fmt.Errorf("duplicate observation: %s", observation.Symbol)
		}
		if observation.Freshness != "REALTIME" && observation.Freshness != "DELAYED" && observation.Freshness != "EOD" {
			return nil, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: stale observation %s", observation.Symbol)
		}
		if observation.WindowType == "" || observation.SessionDate == "" || observation.Source == "" {
			return nil, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: incomplete observation %s", observation.Symbol)
		}
		if _, err := time.Parse(time.RFC3339, observation.ObservedAt); err != nil {
			return nil, fmt.Errorf("invalid observation time for %s", observation.Symbol)
		}
		value, ok := new(big.Rat).SetString(observation.ChangePct)
		if !ok {
			return nil, fmt.Errorf("invalid return for %s", observation.Symbol)
		}
		returns[observation.Symbol] = value
		observations[observation.Symbol] = observation
	}

	indicators := make(FeatureSnapshot, len(indicatorPairs))
	for name, pair := range indicatorPairs {
		left, leftOK := returns[pair[0]]
		right, rightOK := returns[pair[1]]
		if !leftOK || !rightOK {
			return nil, errors.New("SKIPPED_SOURCE_INCOMPLETE: core observation missing")
		}
		leftObservation := observations[pair[0]]
		rightObservation := observations[pair[1]]
		if leftObservation.WindowType != rightObservation.WindowType || leftObservation.SessionDate != rightObservation.SessionDate {
			return nil, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: indicator window mismatch for %s", name)
		}
		indicators[name] = new(big.Rat).Sub(left, right).FloatString(2)
	}
	return indicators, nil
}
