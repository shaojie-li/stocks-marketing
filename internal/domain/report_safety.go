package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	TradeLong  TradeDirection = "LONG"
	TradeShort TradeDirection = "SHORT"
)

type TradeDirection string

type ReportSafetyDecision struct {
	Action                string `json:"action"`
	Route                 string `json:"route"`
	FormalDeliveryAllowed bool   `json:"formal_delivery_allowed"`
	Reason                string `json:"reason"`
}

type reportSafetyInput struct {
	Scores struct {
		Entry struct {
			Value       *json.Number `json:"value"`
			CoveragePct json.Number  `json:"coverage_pct"`
		} `json:"entry"`
	} `json:"scores"`
	Strategy struct {
		Current                string `json:"current"`
		BestStructure          string `json:"best_structure"`
		Rationale              string `json:"rationale"`
		InvalidationConditions []struct {
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"invalidation_conditions"`
	} `json:"strategy"`
}

func EvaluateReportSafety(report []byte) (ReportSafetyDecision, error) {
	decoder := json.NewDecoder(bytes.NewReader(report))
	decoder.UseNumber()
	var input reportSafetyInput
	if err := decoder.Decode(&input); err != nil {
		return ReportSafetyDecision{}, errors.New("decode report safety fields")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ReportSafetyDecision{}, errors.New("report safety input contains trailing data")
	}
	coverage, err := input.Scores.Entry.CoveragePct.Float64()
	if err != nil || coverage < 0 || coverage > 100 {
		return ReportSafetyDecision{}, errors.New("invalid Entry coverage")
	}
	degraded := input.Scores.Entry.Value == nil || coverage < 70
	if degraded {
		if input.Strategy.Current != "OBSERVE" && input.Strategy.Current != "WAIT" {
			return ReportSafetyDecision{}, errors.New("degraded analysis must use OBSERVE or WAIT")
		}
		if input.Strategy.BestStructure != "NO_TRADE" {
			return ReportSafetyDecision{}, errors.New("degraded analysis must use NO_TRADE")
		}
		if len(input.Strategy.InvalidationConditions) != 0 {
			return ReportSafetyDecision{}, errors.New("NO_TRADE must not invent trade invalidation conditions")
		}
		if containsActionableTradeLanguage(input.Strategy.Rationale) {
			return ReportSafetyDecision{}, errors.New("degraded analysis contains actionable trade language")
		}
		return ReportSafetyDecision{
			Action: "NO_ENTRY", Route: "SHADOW_ONLY", FormalDeliveryAllowed: false,
			Reason: "ENTRY_COVERAGE_INSUFFICIENT",
		}, nil
	}
	if input.Strategy.BestStructure != "NO_TRADE" {
		if len(input.Strategy.InvalidationConditions) == 0 {
			return ReportSafetyDecision{}, errors.New("trade structure requires an invalidation condition")
		}
		for _, condition := range input.Strategy.InvalidationConditions {
			if len(condition.EvidenceRefs) == 0 {
				return ReportSafetyDecision{}, errors.New("trade invalidation requires evidence")
			}
		}
	}
	return ReportSafetyDecision{Action: "ENTRY_ALLOWED", Route: "FORMAL", FormalDeliveryAllowed: true, Reason: "ENTRY_GATE_PASSED"}, nil
}

func CatalystEntryValue(eventDirection MarketDirection, state CatalystState, tradeDirection TradeDirection) (string, error) {
	if eventDirection != DirectionBullish && eventDirection != DirectionBearish {
		return "", errors.New("invalid Catalyst event direction")
	}
	if tradeDirection != TradeLong && tradeDirection != TradeShort {
		return "", errors.New("invalid trade direction")
	}
	if state == CatalystNeutral {
		return "1.0", nil
	}
	if state != CatalystAccepted && state != CatalystRejected {
		return "", errors.New("invalid Catalyst state")
	}
	outcome := eventDirection
	if state == CatalystRejected {
		if outcome == DirectionBullish {
			outcome = DirectionBearish
		} else {
			outcome = DirectionBullish
		}
	}
	aligned := outcome == DirectionBullish && tradeDirection == TradeLong || outcome == DirectionBearish && tradeDirection == TradeShort
	if aligned {
		return "2.0", nil
	}
	return "0.0", nil
}

func containsActionableTradeLanguage(value string) bool {
	normalized := strings.ToUpper(" " + value + " ")
	for _, phrase := range []string{"买入", "卖出", "开仓", "止损", "目标价", "仓位", "盈亏比", " BUY ", " SELL ", " LONG ", " SHORT "} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func ValidateFormalReport(report []byte) error {
	decision, err := EvaluateReportSafety(report)
	if err != nil {
		return err
	}
	if !decision.FormalDeliveryAllowed {
		return fmt.Errorf("analysis report is shadow-only: %s", decision.Reason)
	}
	return nil
}
