package domain

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"
)

const (
	CatalystRuleVersion = "catalyst-acceptance/1.0.0"

	CatalystCategoryDerivativeTradingLoss = "DERIVATIVE_TRADING_LOSS_OCCURRED"

	CatalystReasonNotProvided            = "NOT_PROVIDED"
	CatalystReasonNoRecentSupportedEvent = "NO_RECENT_SUPPORTED_EVENT"
	CatalystReasonRevisionUnsupported    = "REVISION_UNSUPPORTED"
	CatalystReasonSourceIncomplete       = "SOURCE_INCOMPLETE"
)

type CatalystEvent struct {
	SourceEventID     string          `json:"source_event_id"`
	PublishedAt       string          `json:"published_at"`
	EventAt           string          `json:"event_at"`
	Source            string          `json:"source"`
	SourceTier        string          `json:"source_tier"`
	OriginalSource    string          `json:"original_source"`
	Category          string          `json:"category"`
	AffectedAssets    []string        `json:"affected_assets"`
	Importance        string          `json:"importance"`
	FactStatus        string          `json:"fact_status"`
	ExpectedDirection MarketDirection `json:"expected_direction"`
	EvidenceRefs      []string        `json:"evidence_refs"`
}

type CatalystPriceWindow struct {
	TargetSymbol        string   `json:"target_symbol"`
	BenchmarkSymbol     string   `json:"benchmark_symbol"`
	WindowStart         string   `json:"window_start"`
	WindowEnd           string   `json:"window_end"`
	TargetStartPrice    string   `json:"target_start_price"`
	TargetEndPrice      string   `json:"target_end_price"`
	BenchmarkStartPrice string   `json:"benchmark_start_price"`
	BenchmarkEndPrice   string   `json:"benchmark_end_price"`
	EvidenceRefs        []string `json:"evidence_refs"`
}

type CatalystEvaluation struct {
	RuleVersion          string              `json:"rule_version"`
	Availability         string              `json:"availability"`
	Reason               string              `json:"reason,omitempty"`
	Event                CatalystEvent       `json:"event,omitempty"`
	AsOf                 string              `json:"as_of,omitempty"`
	State                CatalystState       `json:"state,omitempty"`
	Preliminary          bool                `json:"preliminary"`
	PriceWindow          CatalystPriceWindow `json:"price_window,omitempty"`
	TargetReturnPct      string              `json:"target_return_pct,omitempty"`
	BenchmarkReturnPct   string              `json:"benchmark_return_pct,omitempty"`
	DirectionalReturnPct string              `json:"directional_return_pct,omitempty"`
	DirectionalAlphaPP   string              `json:"directional_alpha_pp,omitempty"`
	EvidenceRefs         []string            `json:"evidence_refs"`
}

func UnavailableCatalyst(reason string, event CatalystEvent) CatalystEvaluation {
	return CatalystWithAvailability(AvailabilityUnavailable, reason, event)
}

func CatalystWithAvailability(availability, reason string, event CatalystEvent) CatalystEvaluation {
	evidence := slices.Clone(event.EvidenceRefs)
	if evidence == nil {
		evidence = []string{}
	}
	return CatalystEvaluation{
		RuleVersion: CatalystRuleVersion, Availability: availability,
		Reason: reason, Event: event, EvidenceRefs: evidence,
	}
}

func EvaluateCatalyst(event CatalystEvent, asOfRaw string, window CatalystPriceWindow) (CatalystEvaluation, error) {
	event, eventAt, err := normalizeCatalystEvent(event)
	if err != nil {
		return CatalystEvaluation{}, err
	}
	asOf, err := time.Parse(time.RFC3339Nano, asOfRaw)
	if err != nil || asOf.Before(eventAt) {
		return CatalystEvaluation{}, errors.New("invalid Catalyst as_of")
	}
	asOf = asOf.UTC()
	start, end, _, preliminary, err := CatalystWindowTimes(eventAt, asOf)
	if err != nil {
		return CatalystEvaluation{}, err
	}
	windowStart, startErr := time.Parse(time.RFC3339Nano, window.WindowStart)
	windowEnd, endErr := time.Parse(time.RFC3339Nano, window.WindowEnd)
	if startErr != nil || endErr != nil || !windowStart.Equal(start) || !windowEnd.Equal(end) {
		return CatalystEvaluation{}, errors.New("Catalyst price window does not match event anchors")
	}
	if len(event.AffectedAssets) != 1 || window.TargetSymbol != event.AffectedAssets[0] || window.BenchmarkSymbol != "xyz:SMSN" {
		return CatalystEvaluation{}, errors.New("invalid Catalyst target or benchmark")
	}
	if err := validateEvidenceRefs(window.EvidenceRefs); err != nil {
		return CatalystEvaluation{}, fmt.Errorf("invalid Catalyst price evidence: %w", err)
	}
	values := []*string{
		&window.TargetStartPrice, &window.TargetEndPrice,
		&window.BenchmarkStartPrice, &window.BenchmarkEndPrice,
	}
	parsed := make([]*big.Rat, len(values))
	for index, value := range values {
		parsed[index], err = parseDecimal(*value)
		if err != nil || parsed[index].Sign() <= 0 {
			return CatalystEvaluation{}, errors.New("Catalyst price must be a positive decimal")
		}
		*value = parsed[index].FloatString(8)
	}
	targetReturn := percentageReturn(parsed[0], parsed[1])
	benchmarkReturn := percentageReturn(parsed[2], parsed[3])
	directionalReturn := new(big.Rat).Set(targetReturn)
	directionalAlpha := new(big.Rat).Sub(targetReturn, benchmarkReturn)
	if event.ExpectedDirection == DirectionBearish {
		directionalReturn.Neg(directionalReturn)
		directionalAlpha.Neg(directionalAlpha)
	}
	state := CatalystNeutral
	acceptedReturn, acceptedAlpha := big.NewRat(1, 2), big.NewRat(1, 4)
	rejectedReturn, rejectedAlpha := big.NewRat(-1, 2), big.NewRat(-1, 4)
	switch {
	case directionalReturn.Cmp(acceptedReturn) >= 0 && directionalAlpha.Cmp(acceptedAlpha) >= 0:
		state = CatalystAccepted
	case directionalReturn.Cmp(rejectedReturn) <= 0 && directionalAlpha.Cmp(rejectedAlpha) <= 0:
		state = CatalystRejected
	}
	window.WindowStart = start.Format(time.RFC3339Nano)
	window.WindowEnd = end.Format(time.RFC3339Nano)
	window.EvidenceRefs = slices.Clone(window.EvidenceRefs)
	slices.Sort(window.EvidenceRefs)
	evidence := appendUnique(nil, event.EvidenceRefs...)
	evidence = appendUnique(evidence, window.EvidenceRefs...)
	slices.Sort(evidence)
	return CatalystEvaluation{
		RuleVersion: CatalystRuleVersion, Availability: AvailabilityAvailable,
		Event: event, AsOf: asOf.Format(time.RFC3339Nano), State: state, Preliminary: preliminary,
		PriceWindow: window, TargetReturnPct: targetReturn.FloatString(8), BenchmarkReturnPct: benchmarkReturn.FloatString(8),
		DirectionalReturnPct: directionalReturn.FloatString(8), DirectionalAlphaPP: directionalAlpha.FloatString(8),
		EvidenceRefs: evidence,
	}, nil
}

func CatalystWindowTimes(eventAt, asOf time.Time) (time.Time, time.Time, time.Time, bool, error) {
	eventAt, asOf = eventAt.UTC(), asOf.UTC()
	if asOf.Before(eventAt) {
		return time.Time{}, time.Time{}, time.Time{}, false, errors.New("Catalyst as_of precedes event")
	}
	theoreticalEnd := eventAt.Add(24 * time.Hour)
	effectiveEnd := theoreticalEnd
	preliminary := asOf.Before(theoreticalEnd)
	if preliminary {
		effectiveEnd = asOf
	}
	windowStart := eventAt.Truncate(time.Minute).Add(-time.Millisecond)
	windowEnd := effectiveEnd.Truncate(time.Minute).Add(-time.Millisecond)
	if windowEnd.Before(windowStart) {
		return time.Time{}, time.Time{}, time.Time{}, false, errors.New("Catalyst window has no completed candle")
	}
	return windowStart, windowEnd, theoreticalEnd, preliminary, nil
}

func normalizeCatalystEvent(event CatalystEvent) (CatalystEvent, time.Time, error) {
	if !validDARTReceipt(event.SourceEventID) || event.Source != "dart" || event.SourceTier != "OFFICIAL" ||
		event.OriginalSource != "https://dart.fss.or.kr/api/link.jsp?rcpNo="+event.SourceEventID ||
		event.Category != CatalystCategoryDerivativeTradingLoss || event.Importance != "HIGH" || event.FactStatus != "CONFIRMED" ||
		event.ExpectedDirection != DirectionBearish {
		return CatalystEvent{}, time.Time{}, errors.New("invalid Catalyst event")
	}
	publishedAt, publishedErr := time.Parse(time.RFC3339Nano, event.PublishedAt)
	eventAt, eventErr := time.Parse(time.RFC3339Nano, event.EventAt)
	if publishedErr != nil || eventErr != nil || !publishedAt.Equal(eventAt) {
		return CatalystEvent{}, time.Time{}, errors.New("invalid Catalyst event time")
	}
	if len(event.AffectedAssets) != 1 || event.AffectedAssets[0] != "xyz:SKHY" || validateEvidenceRefs(event.EvidenceRefs) != nil {
		return CatalystEvent{}, time.Time{}, errors.New("invalid Catalyst event evidence")
	}
	event.PublishedAt = publishedAt.UTC().Format(time.RFC3339Nano)
	event.EventAt = eventAt.UTC().Format(time.RFC3339Nano)
	event.AffectedAssets = slices.Clone(event.AffectedAssets)
	event.EvidenceRefs = slices.Clone(event.EvidenceRefs)
	slices.Sort(event.EvidenceRefs)
	return event, eventAt.UTC(), nil
}

func validDARTReceipt(value string) bool {
	if len(value) != 14 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func percentageReturn(start, end *big.Rat) *big.Rat {
	result := new(big.Rat).Sub(end, start)
	result.Quo(result, start)
	return result.Mul(result, big.NewRat(100, 1))
}
