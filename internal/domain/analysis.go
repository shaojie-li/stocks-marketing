package domain

import (
	"encoding/json"
	"fmt"
	"math/big"
)

type Observation struct {
	ID               string `json:"observation_id"`
	Symbol           string `json:"symbol"`
	Price            string `json:"price"`
	BaselinePrice    string `json:"baseline_price,omitempty"`
	BaselineAt       string `json:"baseline_at,omitempty"`
	ChangePct        string `json:"change_pct"`
	ObservedAt       string `json:"observed_at"`
	MarketStatus     string `json:"market_status"`
	WindowType       string `json:"window_type"`
	SessionDate      string `json:"session_date"`
	Source           string `json:"source"`
	SourceTier       string `json:"source_tier"`
	Freshness        string `json:"freshness"`
	Adjustment       string `json:"adjustment"`
	TheoreticalStart string `json:"theoretical_start,omitempty"`
	WindowStart      string `json:"window_start,omitempty"`
	WindowEnd        string `json:"window_end,omitempty"`
}

type FeatureSnapshot map[string]string

type reportInput struct {
	Observations []Observation `json:"observations"`
	Window       struct {
		Type  string `json:"type"`
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"window"`
}

func ComputeIndicators(report []byte) (FeatureSnapshot, error) {
	var input reportInput
	if err := json.Unmarshal(report, &input); err != nil {
		return nil, fmt.Errorf("decode analysis input: %w", err)
	}
	for index := range input.Observations {
		if input.Observations[index].WindowType == "" {
			input.Observations[index].WindowType = input.Window.Type
		}
		if input.Observations[index].WindowStart == "" {
			input.Observations[index].WindowStart = input.Window.Start
		}
		if input.Observations[index].WindowEnd == "" {
			input.Observations[index].WindowEnd = input.Window.End
		}
	}
	calculated, err := CalculateCoreIndicators(input.Observations)
	if err != nil {
		return nil, err
	}
	indicators := make(FeatureSnapshot, len(indicatorPairs))
	for name, indicator := range calculated.Relative {
		value, _ := new(big.Rat).SetString(indicator.ValuePP)
		indicators[name] = value.FloatString(2)
	}
	return indicators, nil
}
