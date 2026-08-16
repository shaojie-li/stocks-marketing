package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

const ShadowPromptVersion = "shadow-deterministic/1.0.0"

func BuildDegradedShadowReport(bundle AnalysisBundle) ([]byte, error) {
	if bundle.Identity.RuleVersion != GlobalAnalysisRuleVersion || bundle.Identity.Phase != "GLOBAL" || bundle.InputHash == "" {
		return nil, errors.New("invalid shadow Analysis Bundle")
	}
	trendValue, err := strconv.ParseFloat(bundle.Trend.Value, 64)
	if err != nil || trendValue < 0 || trendValue > 10 {
		return nil, errors.New("invalid shadow Trend Score")
	}
	evidenceIDs := map[string]string{
		IndicatorMemoryRelativeStrength:        "ev-memory-rs",
		IndicatorSemiconductorRelativeStrength: "ev-semiconductor-rs",
		IndicatorAIComputeRotation:             "ev-ai-compute-rotation",
		IndicatorSKHYSectorAlpha:               "ev-skhy-sector-alpha",
		IndicatorSKHYMarketAlpha:               "ev-skhy-market-alpha",
		"cross_market":                         "ev-cross-market",
		"trend":                                "ev-trend",
		"catalyst":                             "ev-catalyst",
	}
	evidence := make([]any, 0, 8)
	for _, name := range []string{IndicatorMemoryRelativeStrength, IndicatorSemiconductorRelativeStrength, IndicatorAIComputeRotation, IndicatorSKHYSectorAlpha, IndicatorSKHYMarketAlpha} {
		indicator, ok := bundle.Indicators.Relative[name]
		if !ok || indicator.Availability != AvailabilityAvailable {
			return nil, fmt.Errorf("shadow report requires %s", name)
		}
		evidence = append(evidence, derivedEvidence(evidenceIDs[name], "RELATIVE_STRENGTH", bundle.AsOf, []string{indicator.LeftSymbol, indicator.RightSymbol}, name+" 由同窗口 Observation 确定性计算。"))
	}
	evidence = append(evidence,
		derivedEvidence(evidenceIDs["cross_market"], "CROSS_MARKET", bundle.AsOf, []string{"xyz:MU", "xyz:SMH", "xyz:SKHY", "xyz:SMSN"}, "Cross-Market 由 Memory RS 与 SKHY Sector Alpha 确定性计算。"),
		derivedEvidence(evidenceIDs["trend"], "TREND_SCORE", bundle.AsOf, []string{bundle.Identity.PrimaryAsset}, "Trend Score 由可用组件按冻结权重归一化。"),
	)
	catalystRefs := []string{}
	if bundle.Catalyst.Availability == AvailabilityAvailable {
		catalystRefs = []string{evidenceIDs["catalyst"]}
		evidence = append(evidence, map[string]any{
			"evidence_id": evidenceIDs["catalyst"], "category": "CATALYST_ACCEPTANCE", "fact_status": "CONFIRMED",
			"summary": "DART 官方事件及其 24 小时价格接受由确定性规则评估。", "source": "domain-engine", "source_tier": "DERIVED",
			"published_at": bundle.Catalyst.Event.PublishedAt, "event_at": bundle.Catalyst.Event.EventAt,
			"original_source": bundle.Catalyst.Event.OriginalSource, "importance": bundle.Catalyst.Event.Importance,
			"numeric_context": nil, "affected_assets": bundle.Catalyst.Event.AffectedAssets,
		})
	}
	trendComponents := make([]any, 0, len(bundle.Trend.Components))
	for _, component := range bundle.Trend.Components {
		refs := []string{}
		if component.Availability == AvailabilityAvailable {
			ref := evidenceIDs[component.Name]
			if ref == "" {
				return nil, fmt.Errorf("missing shadow evidence for %s", component.Name)
			}
			refs = []string{ref}
		}
		value, err := optionalReportNumber(component.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid shadow Trend component %s", component.Name)
		}
		trendComponents = append(trendComponents, map[string]any{
			"name": component.Name, "weight": component.Weight, "value": value, "evidence_refs": refs,
		})
	}
	indicators := map[string]any{}
	for _, name := range []string{IndicatorMemoryRelativeStrength, IndicatorSemiconductorRelativeStrength, IndicatorAIComputeRotation, IndicatorSKHYSectorAlpha, IndicatorSKHYMarketAlpha} {
		indicator := bundle.Indicators.Relative[name]
		indicators[name] = map[string]any{
			"availability": indicator.Availability, "value_pp": indicator.ValuePP, "state": indicator.State,
			"window_type": indicator.WindowType, "left_symbol": indicator.LeftSymbol, "right_symbol": indicator.RightSymbol,
			"session_date": indicator.SessionDate, "input_observation_refs": indicator.InputRefs,
		}
	}
	observations := make([]any, 0, len(bundle.Observations))
	for _, observation := range bundle.Observations {
		observations = append(observations, map[string]any{
			"observation_id": observation.ID, "symbol": observation.Symbol, "price": observation.Price,
			"change_pct": observation.ChangePct, "observed_at": observation.ObservedAt, "market_status": observation.MarketStatus,
			"window_type": observation.WindowType, "session_date": observation.SessionDate, "source": observation.Source,
			"source_tier": observation.SourceTier, "freshness": observation.Freshness, "adjustment": observation.Adjustment,
		})
	}
	missing := []string{"scores.fundamental", "scores.entry", "foreign_flow", "crowding"}
	if bundle.PriceStructure.Availability != AvailabilityAvailable {
		missing = append(missing, "price_structure")
	}
	confirmation := shadowConfirmation(bundle, evidenceIDs, catalystRefs)
	report := map[string]any{
		"schema_version": "1.0.0", "analysis_id": "shadow:" + bundle.InputHash, "idempotency_key": "shadow:" + bundle.InputHash,
		"rule_version": GlobalAnalysisRuleVersion, "prompt_version": ShadowPromptVersion, "model": "NOT_INVOKED",
		"phase": "GLOBAL", "primary_asset": bundle.Identity.PrimaryAsset, "as_of": bundle.AsOf,
		"window": map[string]any{"type": bundle.Identity.WindowType, "start": bundle.Identity.WindowStart, "end": bundle.Identity.WindowEnd, "baseline": "CONTINUOUS_24H_PRICE"},
		"scores": map[string]any{
			"fundamental": unavailableScore(fundamentalShadowComponents()),
			"trend":       map[string]any{"value": trendValue, "direction": reportDirection(bundle.Trend.Direction), "coverage_pct": bundle.Trend.CoveragePct, "confidence": bundle.Trend.ConfidenceMax, "components": trendComponents},
			"entry":       unavailableScore(entryShadowComponents()),
		},
		"indicators": indicators,
		"cross_market": map[string]any{
			"availability": bundle.Indicators.CrossMarket.Availability, "state": nullableString(string(bundle.Indicators.CrossMarket.State)),
			"direction": nullableString(string(bundle.Indicators.CrossMarket.Direction)), "us_session_date": bundle.Indicators.CrossMarket.USSessionDate,
			"kr_session_date": bundle.Indicators.CrossMarket.KRSessionDate, "lag_hours": bundle.Indicators.CrossMarket.LagHours,
			"evidence_refs": []string{evidenceIDs[IndicatorMemoryRelativeStrength], evidenceIDs[IndicatorSKHYSectorAlpha]},
		},
		"catalyst": map[string]any{
			"availability": bundle.Catalyst.Availability, "state": nullableString(string(bundle.Catalyst.State)),
			"expected_direction": nullableString(string(bundle.Catalyst.Event.ExpectedDirection)), "preliminary": bundle.Catalyst.Preliminary,
			"directional_return_pct": nullableString(bundle.Catalyst.DirectionalReturnPct), "directional_alpha_pp": nullableString(bundle.Catalyst.DirectionalAlphaPP),
			"evidence_refs": catalystRefs,
		},
		"foreign_flow": map[string]any{"quality_status": "UNAVAILABLE", "direction": nil, "net_buy_value_krw": nil, "traded_value_krw": nil, "flow_ratio_pct": nil, "observed_at": nil, "source": nil, "evidence_refs": []string{}},
		"crowding":     map[string]any{"availability": "UNAVAILABLE", "state": nil, "direction": nil, "available_inputs": 0, "trigger_count": 0, "evidence_refs": []string{}},
		"memory_trend": map[string]any{
			"previous_state": bundle.Memory.PreviousState, "state": bundle.Memory.State, "day_classification": bundle.Memory.Day,
			"transitioned": bundle.Memory.Transitioned, "supportive_streak": bundle.Memory.SupportiveStreak, "adverse_streak": bundle.Memory.AdverseStreak,
			"session_date": bundle.Memory.SessionDate, "reason": bundle.Memory.Reason, "evidence_refs": []string{},
		},
		"confirmation_chain": confirmation,
		"strategy": map[string]any{
			"current": "OBSERVE", "best_structure": "NO_TRADE", "rationale": "Entry 数据覆盖不足，仅提供观察结论。",
			"invalidation_conditions": []any{}, "evidence_refs": []string{evidenceIDs["trend"]},
		},
		"data_audit": map[string]any{
			"freshness": bundle.Quality.DataFreshness, "completeness": "LOW", "completeness_pct": float64(bundle.Trend.CoveragePct) / 3,
			"confidence": "LOW", "missing_fields": missing, "conflicts": []any{},
		},
		"observations": observations, "evidence": evidence,
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, errors.New("encode degraded shadow report")
	}
	decision, err := EvaluateReportSafety(raw)
	if err != nil || decision.FormalDeliveryAllowed || decision.Route != "SHADOW_ONLY" {
		return nil, errors.New("degraded shadow report escaped safety gate")
	}
	return raw, nil
}

func unavailableScore(components []any) map[string]any {
	return map[string]any{"value": nil, "direction": "UNAVAILABLE", "coverage_pct": 0, "confidence": "LOW", "components": components}
}

func fundamentalShadowComponents() []any {
	return unavailableComponents([]struct {
		name   string
		weight int
	}{{"ai_hbm_server_demand", 2}, {"memory_pricing_cycle", 2}, {"supply_discipline", 2}, {"earnings_guidance_revisions", 2}, {"balance_sheet_capex_execution", 1}, {"regulatory_customer_event_risk", 1}})
}

func entryShadowComponents() []any {
	return unavailableComponents([]struct {
		name   string
		weight int
	}{{"extension_pullback", 3}, {"reward_risk", 2}, {"catalyst_acceptance", 2}, {"liquidity_volatility", 1}, {"crowding", 2}})
}

func unavailableComponents(specs []struct {
	name   string
	weight int
}) []any {
	result := make([]any, 0, len(specs))
	for _, spec := range specs {
		result = append(result, map[string]any{"name": spec.name, "weight": spec.weight, "value": nil, "evidence_refs": []string{}})
	}
	return result
}

func shadowConfirmation(bundle AnalysisBundle, evidenceIDs map[string]string, catalystRefs []string) map[string]any {
	steps := []any{
		confirmationStep(1, "xyz:MU 跑赢 xyz:SMH", relativePass(bundle.Indicators.Relative[IndicatorMemoryRelativeStrength]), []string{evidenceIDs[IndicatorMemoryRelativeStrength]}),
		confirmationStep(2, "xyz:SKHY 跑赢 xyz:SMSN", relativePass(bundle.Indicators.Relative[IndicatorSKHYSectorAlpha]), []string{evidenceIDs[IndicatorSKHYSectorAlpha]}),
		confirmationStep(3, "xyz:SKHY 跑赢 xyz:KR200", relativePass(bundle.Indicators.Relative[IndicatorSKHYMarketAlpha]), []string{evidenceIDs[IndicatorSKHYMarketAlpha]}),
		confirmationStep(4, "外资净买 SKHY", "UNAVAILABLE", []string{}),
		confirmationStep(5, "价格接受 Catalyst", catalystStep(bundle.Catalyst), catalystRefs),
		confirmationStep(6, "Trend Score 提高", trendStep(bundle.Trend), []string{evidenceIDs["trend"]}),
	}
	return map[string]any{"status": "INCOMPLETE", "first_break": 4, "steps": steps}
}

func confirmationStep(position int, name, status string, refs []string) map[string]any {
	return map[string]any{"position": position, "name": name, "status": status, "evidence_refs": refs}
}

func relativePass(indicator RelativeIndicator) string {
	if indicator.Availability != AvailabilityAvailable {
		return "UNAVAILABLE"
	}
	if indicator.State == RelativeStrong || indicator.State == RelativePositive {
		return "PASS"
	}
	return "FAIL"
}

func catalystStep(catalyst CatalystEvaluation) string {
	if catalyst.Availability != AvailabilityAvailable {
		return "UNAVAILABLE"
	}
	if catalyst.State == CatalystAccepted {
		return "PASS"
	}
	return "FAIL"
}

func trendStep(trend TrendScore) string {
	if trend.Direction == "" {
		return "UNAVAILABLE"
	}
	if trend.Direction == ScoreDirectionUp {
		return "PASS"
	}
	return "FAIL"
}

func derivedEvidence(id, category, at string, assets []string, summary string) map[string]any {
	return map[string]any{"evidence_id": id, "category": category, "fact_status": "CONFIRMED", "summary": summary, "source": "domain-engine", "source_tier": "DERIVED", "published_at": at, "event_at": at, "original_source": "hyperliquid", "importance": "MEDIUM", "numeric_context": nil, "affected_assets": assets}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func reportDirection(value ScoreDirection) string {
	if value == "" {
		return "UNAVAILABLE"
	}
	return string(value)
}
func optionalReportNumber(value string) (any, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}
