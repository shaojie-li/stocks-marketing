package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

const GlobalAnalysisRuleVersion = "global-analysis/1.1.0"

type AnalysisIdentity struct {
	RuleVersion  string `json:"rule_version"`
	Phase        string `json:"phase"`
	PrimaryAsset string `json:"primary_asset"`
	WindowType   string `json:"window_type"`
	WindowStart  string `json:"window_start"`
	WindowEnd    string `json:"window_end"`
	AsOfBucket   string `json:"as_of_bucket"`
}

type UnavailableSignals struct {
	PriceStructure string `json:"skhy_price_structure"`
	ForeignFlow    string `json:"skhy_foreign_flow"`
}

type AnalysisQuality struct {
	DataFreshness    string     `json:"data_freshness"`
	DataCompleteness string     `json:"data_completeness"`
	ConfidenceMax    Confidence `json:"confidence_max"`
}

type AnalysisBundle struct {
	Identity     AnalysisIdentity      `json:"identity"`
	AsOf         string                `json:"as_of"`
	InputHash    string                `json:"input_hash"`
	Observations []Observation         `json:"observations"`
	Indicators   CoreIndicatorSet      `json:"indicators"`
	Trend        TrendScore            `json:"trend"`
	Unavailable  UnavailableSignals    `json:"unavailable"`
	MemoryDay    MemoryDayResult       `json:"memory_day"`
	Memory       MemoryTrendTransition `json:"memory"`
	Quality      AnalysisQuality       `json:"quality"`
}

type AnalysisBundleInput struct {
	Phase          string
	PrimaryAsset   string
	AsOf           string
	AsOfBucket     string
	Observations   []Observation
	PreviousTrend  *TrendScore
	PreviousMemory *MemoryTrendTransition
}

func BuildAnalysisBundle(input AnalysisBundleInput) (AnalysisBundle, error) {
	if input.Phase == "" || input.PrimaryAsset == "" || len(input.Observations) == 0 {
		return AnalysisBundle{}, errors.New("analysis bundle identity is incomplete")
	}
	asOf, err := time.Parse(time.RFC3339Nano, input.AsOf)
	if err != nil {
		return AnalysisBundle{}, errors.New("invalid analysis as_of")
	}
	asOfBucket, err := time.Parse(time.RFC3339Nano, input.AsOfBucket)
	if err != nil || asOfBucket.After(asOf) {
		return AnalysisBundle{}, errors.New("invalid analysis as_of_bucket")
	}
	input.AsOf = asOf.UTC().Format(time.RFC3339Nano)
	input.AsOfBucket = asOfBucket.UTC().Format(time.RFC3339Nano)

	observations := slices.Clone(input.Observations)
	for index := range observations {
		if err := normalizeObservationTimes(&observations[index]); err != nil {
			return AnalysisBundle{}, err
		}
	}
	slices.SortFunc(observations, func(left, right Observation) int {
		if left.Symbol < right.Symbol {
			return -1
		}
		if left.Symbol > right.Symbol {
			return 1
		}
		return 0
	})
	windowType, windowStart, windowEnd := observations[0].WindowType, observations[0].WindowStart, observations[0].WindowEnd
	sessionDate := ""
	for _, observation := range observations {
		observedAt, parseErr := time.Parse(time.RFC3339Nano, observation.ObservedAt)
		if parseErr != nil || observedAt.After(asOf) {
			return AnalysisBundle{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: observation is after as_of")
		}
		if observation.WindowType != windowType || observation.WindowStart != windowStart || observation.WindowEnd != windowEnd {
			return AnalysisBundle{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: bundle window mismatch")
		}
		if observation.Symbol == input.PrimaryAsset {
			sessionDate = observation.SessionDate
		}
	}
	if sessionDate == "" {
		return AnalysisBundle{}, errors.New("SKIPPED_SOURCE_INCOMPLETE: primary asset observation is missing")
	}

	indicators, err := CalculateCoreIndicators(observations)
	if err != nil {
		return AnalysisBundle{}, err
	}
	trend, err := CalculateTrendScore(TrendScoreInput{Indicators: indicators, Phase: input.Phase, Previous: input.PreviousTrend})
	if err != nil {
		return AnalysisBundle{}, err
	}
	memoryDay, err := ClassifyMemoryDay(MemoryDayInput{Indicators: indicators})
	if err != nil {
		return AnalysisBundle{}, err
	}
	state, supportiveStreak, adverseStreak, lastSessionDate := MemoryTrendWeak, 0, 0, ""
	if input.PreviousMemory != nil {
		state = input.PreviousMemory.State
		supportiveStreak = input.PreviousMemory.SupportiveStreak
		adverseStreak = input.PreviousMemory.AdverseStreak
		lastSessionDate = input.PreviousMemory.SessionDate
	}
	memory, err := AdvanceMemoryTrend(MemoryTransitionInput{
		State: state, Day: memoryDay.Classification, SupportiveStreak: supportiveStreak,
		AdverseStreak: adverseStreak, LastSessionDate: lastSessionDate, SessionDate: sessionDate,
		EvidenceRefs: memoryDay.EvidenceRefs,
	})
	if err != nil {
		return AnalysisBundle{}, err
	}
	identity := AnalysisIdentity{
		RuleVersion: GlobalAnalysisRuleVersion, Phase: input.Phase, PrimaryAsset: input.PrimaryAsset,
		WindowType: windowType, WindowStart: windowStart, WindowEnd: windowEnd, AsOfBucket: input.AsOfBucket,
	}
	hashInput := struct {
		Identity       AnalysisIdentity       `json:"identity"`
		AsOf           string                 `json:"as_of"`
		Observations   []Observation          `json:"observations"`
		PreviousTrend  *TrendScore            `json:"previous_trend,omitempty"`
		PreviousMemory *MemoryTrendTransition `json:"previous_memory,omitempty"`
	}{identity, input.AsOf, observations, input.PreviousTrend, input.PreviousMemory}
	canonical, err := json.Marshal(hashInput)
	if err != nil {
		return AnalysisBundle{}, errors.New("encode analysis bundle input")
	}
	digest := sha256.Sum256(canonical)
	return AnalysisBundle{
		Identity: identity, AsOf: input.AsOf, InputHash: hex.EncodeToString(digest[:]), Observations: observations,
		Indicators: indicators, Trend: trend,
		Unavailable: UnavailableSignals{PriceStructure: AvailabilityUnavailable, ForeignFlow: AvailabilityUnavailable},
		MemoryDay:   memoryDay, Memory: memory,
		Quality: AnalysisQuality{DataFreshness: worstFreshness(observations), DataCompleteness: "MEDIUM", ConfidenceMax: ConfidenceLow},
	}, nil
}

func worstFreshness(observations []Observation) string {
	worst := "REALTIME"
	for _, observation := range observations {
		if observation.Freshness == "EOD" {
			return "EOD"
		}
		if observation.Freshness == "DELAYED" {
			worst = "DELAYED"
		}
	}
	return worst
}

func normalizeObservationTimes(observation *Observation) error {
	fields := []struct {
		name     string
		value    *string
		optional bool
	}{
		{"observed_at", &observation.ObservedAt, false},
		{"window_start", &observation.WindowStart, false},
		{"window_end", &observation.WindowEnd, false},
		{"baseline_at", &observation.BaselineAt, true},
		{"theoretical_start", &observation.TheoreticalStart, true},
	}
	for _, field := range fields {
		if field.optional && *field.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, *field.value)
		if err != nil {
			return errors.New("invalid observation " + field.name)
		}
		*field.value = parsed.UTC().Format(time.RFC3339Nano)
	}
	return nil
}
