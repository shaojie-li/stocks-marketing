package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"slices"
	"time"
)

const GlobalAnalysisRuleVersion = "global-analysis/1.7.0"

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
	Fundamental    string `json:"fundamental"`
	PriceStructure string `json:"skhy_price_structure"`
	ForeignFlow    string `json:"skhy_foreign_flow"`
	Catalyst       string `json:"catalyst"`
	Crowding       string `json:"crowding"`
}

type AnalysisQuality struct {
	DataFreshness    string     `json:"data_freshness"`
	DataCompleteness string     `json:"data_completeness"`
	ConfidenceMax    Confidence `json:"confidence_max"`
}

type AnalysisBundle struct {
	Identity       AnalysisIdentity      `json:"identity"`
	AsOf           string                `json:"as_of"`
	InputHash      string                `json:"input_hash"`
	Observations   []Observation         `json:"observations"`
	Indicators     CoreIndicatorSet      `json:"indicators"`
	PriceStructure PriceStructure        `json:"price_structure"`
	Catalyst       CatalystEvaluation    `json:"catalyst"`
	Crowding       Crowding              `json:"crowding"`
	Fundamental    Fundamental           `json:"fundamental"`
	Trend          TrendScore            `json:"trend"`
	Unavailable    UnavailableSignals    `json:"unavailable"`
	MemoryDay      MemoryDayResult       `json:"memory_day"`
	Memory         MemoryTrendTransition `json:"memory"`
	Quality        AnalysisQuality       `json:"quality"`
}

type AnalysisBundleInput struct {
	Phase          string
	PrimaryAsset   string
	AsOf           string
	AsOfBucket     string
	Observations   []Observation
	PriceStructure PriceStructure
	Catalyst       CatalystEvaluation
	Crowding       Crowding
	Fundamental    Fundamental
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
	priceStructure := input.PriceStructure
	if priceStructure.Availability == "" {
		priceStructure = UnavailablePriceStructure(input.PrimaryAsset, PriceStructureReasonNotProvided, 0, nil)
	}
	if err := normalizePriceStructure(&priceStructure, input.PrimaryAsset, asOf); err != nil {
		return AnalysisBundle{}, err
	}
	catalyst := input.Catalyst
	if catalyst.Availability == "" {
		catalyst = UnavailableCatalyst(CatalystReasonNotProvided, CatalystEvent{})
	}
	if err := normalizeCatalystEvaluation(&catalyst, input.PrimaryAsset, asOf); err != nil {
		return AnalysisBundle{}, err
	}
	crowding := input.Crowding
	if crowding.Availability == "" {
		crowding = UnavailableCrowding(input.PrimaryAsset, CrowdingReasonNotProvided)
	}
	if err := normalizeCrowding(&crowding, input.PrimaryAsset, asOf); err != nil {
		return AnalysisBundle{}, err
	}
	fundamental := input.Fundamental
	if fundamental.Availability == "" {
		fundamental = UnavailableFundamental(input.PrimaryAsset, FundamentalReasonInsufficientCoverage)
	}
	if err := normalizeFundamental(&fundamental, input.PrimaryAsset, asOf); err != nil {
		return AnalysisBundle{}, err
	}
	trend, err := CalculateTrendScore(TrendScoreInput{
		Indicators: indicators, Phase: input.Phase, PriceStructure: priceStructure.State,
		PriceStructureRefs: priceStructure.EvidenceRefs, Previous: input.PreviousTrend,
	})
	if err != nil {
		return AnalysisBundle{}, err
	}
	memoryDay, err := ClassifyMemoryDay(MemoryDayInput{
		Indicators: indicators, Catalyst: catalyst.State, CatalystRefs: catalyst.EvidenceRefs,
		PriceStructure: priceStructure.State, PriceStructureRefs: priceStructure.EvidenceRefs,
		DataConflict: catalyst.Availability == AvailabilityDataConflict,
	})
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
		PriceStructure PriceStructure         `json:"price_structure"`
		Catalyst       CatalystEvaluation     `json:"catalyst"`
		Crowding       Crowding               `json:"crowding"`
		Fundamental    Fundamental            `json:"fundamental"`
		PreviousTrend  *TrendScore            `json:"previous_trend,omitempty"`
		PreviousMemory *MemoryTrendTransition `json:"previous_memory,omitempty"`
	}{identity, input.AsOf, observations, priceStructure, catalyst, crowding, fundamental, input.PreviousTrend, input.PreviousMemory}
	canonical, err := json.Marshal(hashInput)
	if err != nil {
		return AnalysisBundle{}, errors.New("encode analysis bundle input")
	}
	digest := sha256.Sum256(canonical)
	return AnalysisBundle{
		Identity: identity, AsOf: input.AsOf, InputHash: hex.EncodeToString(digest[:]), Observations: observations,
		Indicators: indicators, PriceStructure: priceStructure, Catalyst: catalyst, Crowding: crowding, Fundamental: fundamental, Trend: trend,
		Unavailable: UnavailableSignals{
			Fundamental:    fundamental.Availability,
			PriceStructure: priceStructure.Availability,
			ForeignFlow:    AvailabilityUnavailable,
			Catalyst:       catalyst.Availability,
			Crowding:       crowding.Availability,
		},
		MemoryDay: memoryDay, Memory: memory,
		Quality: AnalysisQuality{DataFreshness: worstFreshness(observations), DataCompleteness: "MEDIUM", ConfidenceMax: ConfidenceLow},
	}, nil
}

func normalizeCatalystEvaluation(catalyst *CatalystEvaluation, primaryAsset string, bundleAsOf time.Time) error {
	if catalyst.RuleVersion != CatalystRuleVersion || catalyst.Reason == "" && catalyst.Availability != AvailabilityAvailable {
		return errors.New("invalid Catalyst availability")
	}
	if catalyst.Availability == AvailabilityUnavailable || catalyst.Availability == AvailabilityDataConflict {
		if catalyst.State != "" || catalyst.AsOf != "" || catalyst.Preliminary ||
			catalyst.TargetReturnPct != "" || catalyst.BenchmarkReturnPct != "" ||
			catalyst.DirectionalReturnPct != "" || catalyst.DirectionalAlphaPP != "" ||
			!reflect.DeepEqual(catalyst.PriceWindow, CatalystPriceWindow{}) {
			return errors.New("invalid unavailable Catalyst")
		}
		if reflect.DeepEqual(catalyst.Event, CatalystEvent{}) {
			if len(catalyst.EvidenceRefs) != 0 {
				return errors.New("invalid unavailable Catalyst evidence")
			}
			catalyst.EvidenceRefs = []string{}
			return nil
		}
		event, eventAt, err := normalizeCatalystEvent(catalyst.Event)
		if err != nil || eventAt.After(bundleAsOf) || event.AffectedAssets[0] != primaryAsset {
			return errors.New("invalid unavailable Catalyst event")
		}
		evidence := slices.Clone(catalyst.EvidenceRefs)
		slices.Sort(evidence)
		if !slices.Equal(evidence, event.EvidenceRefs) {
			return errors.New("invalid unavailable Catalyst evidence")
		}
		catalyst.Event = event
		catalyst.EvidenceRefs = evidence
		return nil
	}
	if catalyst.Availability != AvailabilityAvailable || catalyst.Reason != "" {
		return errors.New("invalid available Catalyst")
	}
	recomputed, err := EvaluateCatalyst(catalyst.Event, catalyst.AsOf, catalyst.PriceWindow)
	if err != nil {
		return err
	}
	catalystAsOf, err := time.Parse(time.RFC3339Nano, recomputed.AsOf)
	if err != nil || catalystAsOf.After(bundleAsOf) || recomputed.Event.AffectedAssets[0] != primaryAsset {
		return errors.New("invalid Catalyst bundle context")
	}
	evidence := slices.Clone(catalyst.EvidenceRefs)
	slices.Sort(evidence)
	if catalyst.State != recomputed.State || catalyst.Preliminary != recomputed.Preliminary ||
		catalyst.TargetReturnPct != recomputed.TargetReturnPct || catalyst.BenchmarkReturnPct != recomputed.BenchmarkReturnPct ||
		catalyst.DirectionalReturnPct != recomputed.DirectionalReturnPct || catalyst.DirectionalAlphaPP != recomputed.DirectionalAlphaPP ||
		!slices.Equal(evidence, recomputed.EvidenceRefs) {
		return errors.New("inconsistent Catalyst evaluation")
	}
	*catalyst = recomputed
	return nil
}

func normalizePriceStructure(priceStructure *PriceStructure, primaryAsset string, asOf time.Time) error {
	priceStructure.EvidenceRefs = slices.Clone(priceStructure.EvidenceRefs)
	slices.Sort(priceStructure.EvidenceRefs)
	if priceStructure.Availability == AvailabilityUnavailable {
		if priceStructure.Symbol != primaryAsset || priceStructure.Interval != "1d" || priceStructure.CompletedBars < 0 || priceStructure.State != "" || priceStructure.Reason == "" ||
			priceStructure.WindowStart != "" || priceStructure.WindowEnd != "" || priceStructure.Close != "" || priceStructure.EMA20 != "" || priceStructure.EMA50 != "" || priceStructure.ATR14 != "" || priceStructure.SupportLow20 != "" {
			return errors.New("invalid unavailable Price Structure")
		}
		return nil
	}
	if priceStructure.Availability != AvailabilityAvailable || priceStructure.Reason != "" || priceStructure.Symbol != primaryAsset || priceStructure.Interval != "1d" || priceStructure.CompletedBars < 50 {
		return errors.New("invalid available Price Structure")
	}
	if _, available, err := priceStructureFraction(priceStructure.State); err != nil || !available || validateEvidenceRefs(priceStructure.EvidenceRefs) != nil {
		return errors.New("invalid available Price Structure")
	}
	windowStart, startErr := time.Parse(time.RFC3339Nano, priceStructure.WindowStart)
	windowEnd, endErr := time.Parse(time.RFC3339Nano, priceStructure.WindowEnd)
	if startErr != nil || endErr != nil || !windowStart.Before(windowEnd) || !windowEnd.Before(asOf) {
		return errors.New("invalid Price Structure window")
	}
	priceStructure.WindowStart = windowStart.UTC().Format(time.RFC3339Nano)
	priceStructure.WindowEnd = windowEnd.UTC().Format(time.RFC3339Nano)
	for _, value := range []*string{&priceStructure.Close, &priceStructure.EMA20, &priceStructure.EMA50, &priceStructure.ATR14, &priceStructure.SupportLow20} {
		parsed, err := parseDecimal(*value)
		if err != nil || parsed.Sign() <= 0 {
			return errors.New("invalid Price Structure decimal")
		}
		*value = parsed.FloatString(8)
	}
	return nil
}

func normalizeCrowding(crowding *Crowding, primaryAsset string, asOf time.Time) error {
	if crowding.RuleVersion != CrowdingRuleVersion || crowding.Symbol != primaryAsset || len(crowding.Components) != 5 {
		return errors.New("invalid Crowding identity")
	}
	names := []string{CrowdingInputFunding, CrowdingInputOpenInterest, CrowdingInputPremium, CrowdingInputPriceExtension, CrowdingInputVolume}
	available, triggers := 0, 0
	for index := range crowding.Components {
		component := &crowding.Components[index]
		if component.Name != names[index] {
			return errors.New("invalid Crowding component order")
		}
		component.EvidenceRefs = slices.Clone(component.EvidenceRefs)
		slices.Sort(component.EvidenceRefs)
		if len(component.EvidenceRefs) > 0 && validateEvidenceRefs(component.EvidenceRefs) != nil {
			return errors.New("invalid Crowding evidence")
		}
		if component.CurrentValue != "" {
			value, err := parseDecimal(component.CurrentValue)
			if err != nil {
				return errors.New("invalid Crowding current value")
			}
			component.CurrentValue = value.FloatString(8)
		}
		if component.Availability != AvailabilityAvailable {
			if component.Reason == "" || component.Triggered || component.Percentile != "" || component.WindowStart != "" || component.WindowEnd != "" || component.Direction != "" {
				return errors.New("invalid unavailable Crowding component")
			}
			if component.Availability != AvailabilityUnavailable && component.Availability != AvailabilityStale {
				return errors.New("invalid Crowding availability")
			}
			if component.Name != CrowdingInputOpenInterest && component.CurrentValue != "" {
				return errors.New("unavailable Crowding component has a value")
			}
			continue
		}
		if component.Reason != "" || component.CurrentValue == "" || validateEvidenceRefs(component.EvidenceRefs) != nil {
			return errors.New("invalid available Crowding component")
		}
		start, startErr := time.Parse(time.RFC3339Nano, component.WindowStart)
		end, endErr := time.Parse(time.RFC3339Nano, component.WindowEnd)
		if startErr != nil || endErr != nil || !start.Before(end) || end.After(asOf) {
			return errors.New("invalid Crowding window")
		}
		component.WindowStart = start.UTC().Format(time.RFC3339Nano)
		component.WindowEnd = end.UTC().Format(time.RFC3339Nano)
		value, _ := parseDecimal(component.CurrentValue)
		switch component.Name {
		case CrowdingInputFunding, CrowdingInputPremium:
			percentile, err := parseDecimal(component.Percentile)
			if err != nil || percentile.Sign() < 0 || percentile.Cmp(big.NewRat(100, 1)) > 0 || component.Triggered != (percentile.Cmp(big.NewRat(75, 1)) >= 0) || component.Direction != directionFor(value) {
				return errors.New("invalid Crowding percentile component")
			}
			component.Percentile = percentile.FloatString(8)
		case CrowdingInputPriceExtension:
			if component.Percentile != "" || component.Triggered != (absolute(value).Cmp(big.NewRat(3, 2)) >= 0) || component.Direction != directionFor(value) {
				return errors.New("invalid Crowding extension")
			}
		case CrowdingInputVolume:
			percentile, err := parseDecimal(component.Percentile)
			if err != nil || percentile.Sign() < 0 || percentile.Cmp(big.NewRat(100, 1)) > 0 || component.Triggered != (percentile.Cmp(big.NewRat(75, 1)) >= 0) || component.Direction != "" {
				return errors.New("invalid Crowding volume")
			}
			component.Percentile = percentile.FloatString(8)
		default:
			return errors.New("available historical OI is unsupported")
		}
		available++
		if component.Triggered {
			triggers++
		}
	}
	if crowding.AvailableInputs != available || crowding.TriggerCount != triggers {
		return errors.New("inconsistent Crowding coverage")
	}
	refs := crowdingEvidenceRefs(crowding.Components)
	providedRefs := slices.Clone(crowding.EvidenceRefs)
	slices.Sort(providedRefs)
	if !slices.Equal(refs, providedRefs) {
		return errors.New("inconsistent Crowding evidence")
	}
	crowding.EvidenceRefs = refs
	if available < 4 {
		if crowding.Availability != AvailabilityUnavailable || crowding.Reason == "" || crowding.State != "" || crowding.Direction != "" {
			return errors.New("invalid unavailable Crowding")
		}
		return nil
	}
	extension := crowding.Component(CrowdingInputPriceExtension)
	if crowding.Availability != AvailabilityAvailable || crowding.Reason != "" || crowding.State != crowdingState(triggers, extension.CurrentValue) || crowding.Direction != crowdingDirection(crowding.Component(CrowdingInputFunding).Direction, crowding.Component(CrowdingInputPremium).Direction, extension.Direction) {
		return errors.New("inconsistent available Crowding")
	}
	return nil
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
