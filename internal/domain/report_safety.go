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

type confirmationChainStep struct {
	Position     int      `json:"position"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type reportSafetyInput struct {
	Scores struct {
		Entry struct {
			Value       *json.Number `json:"value"`
			CoveragePct json.Number  `json:"coverage_pct"`
		} `json:"entry"`
	} `json:"scores"`
	ConfirmationChain struct {
		Status     string                  `json:"status"`
		FirstBreak *int                    `json:"first_break"`
		Steps      []confirmationChainStep `json:"steps"`
	} `json:"confirmation_chain"`
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
	confirmationComplete, err := validateConfirmationChain(input.ConfirmationChain.Status, input.ConfirmationChain.FirstBreak, input.ConfirmationChain.Steps)
	if err != nil {
		return ReportSafetyDecision{}, err
	}
	reason := ""
	if input.Scores.Entry.Value == nil || coverage < 70 {
		reason = "ENTRY_COVERAGE_INSUFFICIENT"
	} else if !confirmationComplete {
		reason = "CONFIRMATION_CHAIN_INCOMPLETE"
	} else if input.Strategy.BestStructure == "NO_TRADE" {
		reason = "STRATEGY_NO_TRADE"
	}
	if reason != "" {
		if err := validateShadowStrategy(input); err != nil {
			return ReportSafetyDecision{}, err
		}
		return ReportSafetyDecision{
			Action: "NO_ENTRY", Route: "SHADOW_ONLY", FormalDeliveryAllowed: false,
			Reason: reason,
		}, nil
	}
	if len(input.Strategy.InvalidationConditions) == 0 {
		return ReportSafetyDecision{}, errors.New("trade structure requires an invalidation condition")
	}
	for _, condition := range input.Strategy.InvalidationConditions {
		if validateEvidenceRefs(condition.EvidenceRefs) != nil {
			return ReportSafetyDecision{}, errors.New("trade invalidation requires valid evidence")
		}
	}
	return ReportSafetyDecision{Action: "ENTRY_ALLOWED", Route: "FORMAL", FormalDeliveryAllowed: true, Reason: "ENTRY_GATE_PASSED"}, nil
}

func validateConfirmationChain(status string, firstBreak *int, steps []confirmationChainStep) (bool, error) {
	if len(steps) != 6 || status != "COMPLETE" && status != "BROKEN" && status != "INCOMPLETE" {
		return false, errors.New("invalid confirmation chain")
	}
	statuses := make([]string, 6)
	hasFail, hasUnavailable := false, false
	for _, step := range steps {
		if step.Position < 1 || step.Position > 6 || statuses[step.Position-1] != "" || step.Status != "PASS" && step.Status != "FAIL" && step.Status != "UNAVAILABLE" {
			return false, errors.New("invalid confirmation chain step")
		}
		if len(step.EvidenceRefs) > 0 && validateEvidenceRefs(step.EvidenceRefs) != nil || step.Status != "UNAVAILABLE" && len(step.EvidenceRefs) == 0 {
			return false, errors.New("invalid confirmation chain evidence")
		}
		hasFail = hasFail || step.Status == "FAIL"
		hasUnavailable = hasUnavailable || step.Status == "UNAVAILABLE"
		statuses[step.Position-1] = step.Status
	}
	firstNonPass := 0
	for index, stepStatus := range statuses {
		if stepStatus != "PASS" {
			firstNonPass = index + 1
			break
		}
	}
	if status == "COMPLETE" {
		if firstBreak != nil || firstNonPass != 0 {
			return false, errors.New("invalid complete confirmation chain")
		}
		return true, nil
	}
	if firstBreak == nil || *firstBreak != firstNonPass || firstNonPass == 0 {
		return false, errors.New("invalid incomplete confirmation chain")
	}
	if status == "BROKEN" && !hasFail || status == "INCOMPLETE" && (!hasUnavailable || hasFail) {
		return false, errors.New("inconsistent confirmation chain status")
	}
	return false, nil
}

func validateShadowStrategy(input reportSafetyInput) error {
	if input.Strategy.Current != "OBSERVE" && input.Strategy.Current != "WAIT" {
		return errors.New("degraded analysis must use OBSERVE or WAIT")
	}
	if input.Strategy.BestStructure != "NO_TRADE" {
		return errors.New("degraded analysis must use NO_TRADE")
	}
	if len(input.Strategy.InvalidationConditions) != 0 {
		return errors.New("NO_TRADE must not invent trade invalidation conditions")
	}
	if containsActionableTradeLanguage(input.Strategy.Rationale) {
		return errors.New("degraded analysis contains actionable trade language")
	}
	return nil
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
