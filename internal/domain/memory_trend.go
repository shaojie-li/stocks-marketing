package domain

import (
	"errors"
	"fmt"
	"time"
)

const (
	CatalystAccepted CatalystState = "ACCEPTED"
	CatalystNeutral  CatalystState = "NEUTRAL"
	CatalystRejected CatalystState = "REJECTED"

	MemoryDaySupportive      MemoryDayClassification = "SUPPORTIVE"
	MemoryDayNeutral         MemoryDayClassification = "NEUTRAL_DAY"
	MemoryDayAdverse         MemoryDayClassification = "ADVERSE"
	MemoryDayHardFailure     MemoryDayClassification = "HARD_FAILURE"
	MemoryDayMarketClosed    MemoryDayClassification = "MARKET_CLOSED"
	MemoryDayDataUnavailable MemoryDayClassification = "DATA_UNAVAILABLE"

	MemoryTrendWeak             MemoryTrendState = "WEAK"
	MemoryTrendImproving        MemoryTrendState = "IMPROVING"
	MemoryTrendConfirmed        MemoryTrendState = "CONFIRMED"
	MemoryTrendStrong           MemoryTrendState = "STRONG"
	MemoryTrendPersistentStrong MemoryTrendState = "PERSISTENT_STRONG"
)

type CatalystState string
type MemoryDayClassification string
type MemoryTrendState string

type MemoryDayInput struct {
	Indicators         CoreIndicatorSet
	ForeignFlow        ForeignFlowDirection
	Catalyst           CatalystState
	PriceStructure     PriceStructureState
	ForeignFlowRefs    []string
	CatalystRefs       []string
	PriceStructureRefs []string
	MarketClosed       bool
	DataConflict       bool
}

type MemoryDayResult struct {
	Classification MemoryDayClassification `json:"classification"`
	PositiveCount  int                     `json:"positive_count"`
	NegativeCount  int                     `json:"negative_count"`
	ConfidenceMax  Confidence              `json:"confidence_max"`
	EvidenceRefs   []string                `json:"evidence_refs"`
}

type MemoryTransitionInput struct {
	State            MemoryTrendState
	Day              MemoryDayClassification
	SupportiveStreak int
	AdverseStreak    int
	LastSessionDate  string
	SessionDate      string
	EvidenceRefs     []string
}

type MemoryTrendTransition struct {
	RuleVersion      string                  `json:"rule_version"`
	PreviousState    MemoryTrendState        `json:"previous_state"`
	State            MemoryTrendState        `json:"state"`
	Day              MemoryDayClassification `json:"day_classification"`
	Transitioned     bool                    `json:"transitioned"`
	SupportiveStreak int                     `json:"supportive_streak"`
	AdverseStreak    int                     `json:"adverse_streak"`
	SessionDate      string                  `json:"session_date"`
	Reason           string                  `json:"reason"`
	ConfidenceMax    Confidence              `json:"confidence_max"`
	EvidenceRefs     []string                `json:"evidence_refs"`
}

var memoryStates = []MemoryTrendState{
	MemoryTrendWeak,
	MemoryTrendImproving,
	MemoryTrendConfirmed,
	MemoryTrendStrong,
	MemoryTrendPersistentStrong,
}

func ClassifyMemoryDay(input MemoryDayInput) (MemoryDayResult, error) {
	if input.MarketClosed {
		return MemoryDayResult{Classification: MemoryDayMarketClosed, ConfidenceMax: ConfidenceLow, EvidenceRefs: []string{}}, nil
	}
	if input.DataConflict {
		return MemoryDayResult{Classification: MemoryDayDataUnavailable, ConfidenceMax: ConfidenceLow, EvidenceRefs: []string{}}, nil
	}
	names := []string{IndicatorMemoryRelativeStrength, IndicatorSemiconductorRelativeStrength, IndicatorSKHYSectorAlpha, IndicatorSKHYMarketAlpha}
	states := make([]RelativeStrengthState, 0, len(names))
	for _, name := range names {
		indicator, ok := input.Indicators.Relative[name]
		if !ok || indicator.Availability != AvailabilityAvailable {
			return MemoryDayResult{Classification: MemoryDayDataUnavailable, ConfidenceMax: ConfidenceLow, EvidenceRefs: []string{}}, nil
		}
		if relativeDirection(indicator.State) == 0 && indicator.State != RelativeNeutral {
			return MemoryDayResult{}, fmt.Errorf("invalid Relative Strength state for %s", name)
		}
		states = append(states, indicator.State)
	}
	if input.Indicators.CrossMarket.Availability != AvailabilityAvailable || input.ForeignFlow == "" || input.Catalyst == "" || input.PriceStructure == "" {
		return MemoryDayResult{Classification: MemoryDayDataUnavailable, ConfidenceMax: ConfidenceLow, EvidenceRefs: []string{}}, nil
	}
	if _, _, err := crossMarketTrendFraction(input.Indicators.CrossMarket); err != nil {
		return MemoryDayResult{}, err
	}
	if _, _, err := foreignFlowFraction(input.ForeignFlow); err != nil {
		return MemoryDayResult{}, err
	}
	if _, _, err := priceStructureFraction(input.PriceStructure); err != nil {
		return MemoryDayResult{}, err
	}
	if input.Catalyst != CatalystAccepted && input.Catalyst != CatalystNeutral && input.Catalyst != CatalystRejected {
		return MemoryDayResult{}, errors.New("invalid Catalyst state")
	}
	for name, refs := range map[string][]string{
		"Foreign Flow":    input.ForeignFlowRefs,
		"Catalyst":        input.CatalystRefs,
		"Price Structure": input.PriceStructureRefs,
	} {
		if err := validateEvidenceRefs(refs); err != nil {
			return MemoryDayResult{}, fmt.Errorf("%s evidence: %w", name, err)
		}
	}

	positive, negative := 0, 0
	for _, state := range states {
		switch relativeDirection(state) {
		case 1:
			positive++
		case -1:
			negative++
		}
	}
	cross := input.Indicators.CrossMarket
	if cross.State == CrossMarketConfirmed && cross.Direction == DirectionBullish {
		positive++
	} else if cross.State == CrossMarketDivergence || (cross.State == CrossMarketConfirmed && cross.Direction == DirectionBearish) {
		negative++
	}
	if input.ForeignFlow == ForeignFlowNetBuy {
		positive++
	} else if input.ForeignFlow == ForeignFlowNetSell {
		negative++
	}
	evidenceRefs := make([]string, 0)
	for _, name := range names {
		evidenceRefs = appendUnique(evidenceRefs, input.Indicators.Relative[name].InputRefs...)
	}
	evidenceRefs = appendUnique(evidenceRefs, input.Indicators.CrossMarket.EvidenceRefs...)
	evidenceRefs = appendUnique(evidenceRefs, input.ForeignFlowRefs...)
	evidenceRefs = appendUnique(evidenceRefs, input.CatalystRefs...)
	evidenceRefs = appendUnique(evidenceRefs, input.PriceStructureRefs...)
	result := MemoryDayResult{PositiveCount: positive, NegativeCount: negative, ConfidenceMax: ConfidenceHigh, EvidenceRefs: evidenceRefs}
	switch {
	case cross.State == CrossMarketDivergence && input.ForeignFlow == ForeignFlowNetSell && (input.Catalyst == CatalystRejected || input.PriceStructure == PriceStructureBroken):
		result.Classification = MemoryDayHardFailure
	case negative >= 4 || (cross.State == CrossMarketDivergence && input.ForeignFlow == ForeignFlowNetSell):
		result.Classification = MemoryDayAdverse
	case positive >= 4 && negative <= 1 && input.Catalyst != CatalystRejected:
		result.Classification = MemoryDaySupportive
	default:
		result.Classification = MemoryDayNeutral
	}
	return result, nil
}

func appendUnique(destination []string, refs ...string) []string {
	seen := make(map[string]struct{}, len(destination)+len(refs))
	for _, ref := range destination {
		seen[ref] = struct{}{}
	}
	for _, ref := range refs {
		if _, exists := seen[ref]; !exists {
			destination = append(destination, ref)
			seen[ref] = struct{}{}
		}
	}
	return destination
}

func AdvanceMemoryTrend(input MemoryTransitionInput) (MemoryTrendTransition, error) {
	stateIndex := memoryStateIndex(input.State)
	if stateIndex < 0 {
		return MemoryTrendTransition{}, errors.New("invalid Memory trend state")
	}
	if !validMemoryDay(input.Day) || input.SupportiveStreak < 0 || input.AdverseStreak < 0 {
		return MemoryTrendTransition{}, errors.New("invalid Memory transition input")
	}
	if input.Day != MemoryDayMarketClosed && input.Day != MemoryDayDataUnavailable {
		if err := validateEvidenceRefs(input.EvidenceRefs); err != nil {
			return MemoryTrendTransition{}, fmt.Errorf("Memory transition evidence: %w", err)
		}
	} else if len(input.EvidenceRefs) > 0 {
		if err := validateEvidenceRefs(input.EvidenceRefs); err != nil {
			return MemoryTrendTransition{}, fmt.Errorf("Memory transition evidence: %w", err)
		}
	}
	sessionDate, err := time.Parse(time.DateOnly, input.SessionDate)
	if err != nil {
		return MemoryTrendTransition{}, errors.New("invalid Memory session date")
	}
	if input.LastSessionDate != "" {
		lastSessionDate, err := time.Parse(time.DateOnly, input.LastSessionDate)
		if err != nil || !sessionDate.After(lastSessionDate) {
			return MemoryTrendTransition{}, errors.New("Memory session date is not after previous state")
		}
	}
	result := MemoryTrendTransition{
		RuleVersion: GlobalAnalysisRuleVersion, PreviousState: input.State, State: input.State,
		Day: input.Day, SupportiveStreak: input.SupportiveStreak, AdverseStreak: input.AdverseStreak,
		SessionDate: input.SessionDate, ConfidenceMax: ConfidenceHigh, EvidenceRefs: input.EvidenceRefs,
	}
	switch input.Day {
	case MemoryDaySupportive:
		result.AdverseStreak = 0
		if stateIndex < len(memoryStates)-1 {
			result.SupportiveStreak++
			thresholds := []int{2, 3, 3, 5}
			if result.SupportiveStreak >= thresholds[stateIndex] {
				result.State = memoryStates[stateIndex+1]
				result.SupportiveStreak = 0
			}
		}
		result.Reason = "SUPPORTIVE 交易日累计升级证据"
	case MemoryDayAdverse:
		result.SupportiveStreak = 0
		if stateIndex > 0 {
			result.AdverseStreak++
			if result.AdverseStreak >= 2 {
				result.State = memoryStates[stateIndex-1]
				result.AdverseStreak = 0
			}
		}
		result.Reason = "ADVERSE 交易日累计降级证据"
	case MemoryDayHardFailure:
		result.SupportiveStreak, result.AdverseStreak = 0, 0
		if stateIndex > 0 {
			result.State = memoryStates[stateIndex-1]
		}
		result.Reason = "HARD_FAILURE 触发单级降级"
	case MemoryDayNeutral:
		result.SupportiveStreak, result.AdverseStreak = 0, 0
		result.Reason = "NEUTRAL_DAY 保持状态并清零 streak"
	case MemoryDayMarketClosed:
		result.Reason = "MARKET_CLOSED 保持状态和 streak"
	case MemoryDayDataUnavailable:
		result.SupportiveStreak, result.AdverseStreak = 0, 0
		result.ConfidenceMax = ConfidenceLow
		result.Reason = "DATA_UNAVAILABLE 保持状态并清零 streak"
	}
	result.Transitioned = result.State != result.PreviousState
	return result, nil
}

func memoryStateIndex(state MemoryTrendState) int {
	for index, candidate := range memoryStates {
		if state == candidate {
			return index
		}
	}
	return -1
}

func validMemoryDay(day MemoryDayClassification) bool {
	switch day {
	case MemoryDaySupportive, MemoryDayNeutral, MemoryDayAdverse, MemoryDayHardFailure, MemoryDayMarketClosed, MemoryDayDataUnavailable:
		return true
	default:
		return false
	}
}
