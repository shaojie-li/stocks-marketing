package domain

import (
	"errors"
	"math/big"
	"regexp"
	"time"
)

func normalizeFundamental(value *Fundamental, symbol string, asOf time.Time) error {
	if value.RuleVersion != FundamentalRuleVersion || value.Symbol != symbol || len(value.Components) != 6 {
		return errors.New("invalid Fundamental identity")
	}
	if value.Availability == AvailabilityUnavailable {
		if value.Reason == "" || value.Value != "" || value.CoveragePct >= 70 {
			return errors.New("invalid unavailable Fundamental")
		}
		return nil
	}
	if value.Availability != AvailabilityAvailable || value.Reason != "" || value.CoveragePct != 90 || value.FactStatus != "REPORTED" || validateEvidenceRefs(value.EvidenceRefs) != nil {
		return errors.New("invalid available Fundamental")
	}
	published, err := time.Parse(time.RFC3339Nano, value.PublishedAt)
	if err != nil || published.After(asOf) {
		return errors.New("invalid Fundamental time")
	}
	earned := new(big.Rat)
	weight := 0
	validStates := map[string]bool{"STRONG_NEGATIVE": true, "NEGATIVE": true, "NEUTRAL": true, "POSITIVE": true, "STRONG_POSITIVE": true}
	for index, component := range value.Components {
		if index == 5 {
			if component.Availability != AvailabilityUnavailable {
				return errors.New("invalid Fundamental risk component")
			}
			continue
		}
		if component.Availability != AvailabilityAvailable || !validStates[component.State] || validateEvidenceRefs(component.EvidenceRefs) != nil {
			return errors.New("invalid Fundamental component")
		}
		fraction := fundamentalFraction(component.State)
		earned.Add(earned, new(big.Rat).Mul(fraction, big.NewRat(int64(component.Weight), 1)))
		weight += component.Weight
	}
	if weight != 9 || new(big.Rat).Mul(earned, big.NewRat(10, 9)).FloatString(1) != value.Value {
		return errors.New("inconsistent Fundamental score")
	}
	return nil
}

const (
	FundamentalRuleVersion                = "fundamental/1.0.0"
	FundamentalAIDemand                   = "ai_hbm_server_demand"
	FundamentalPricingCycle               = "memory_pricing_cycle"
	FundamentalSupplyDiscipline           = "supply_discipline"
	FundamentalEarnings                   = "earnings_guidance_revisions"
	FundamentalBalanceSheetCapEx          = "balance_sheet_capex_execution"
	FundamentalRegulatoryRisk             = "regulatory_customer_event_risk"
	FundamentalReasonInsufficientCoverage = "INSUFFICIENT_COVERAGE"
	FundamentalReasonInvalidInput         = "INVALID_INPUT"
	FundamentalReasonNoDeterministicRule  = "NO_DETERMINISTIC_RULE"
	FundamentalReasonSourceError          = "SOURCE_ERROR"
	DirectionIncrease                     = "INCREASE"
	DirectionDecrease                     = "DECREASE"
)

type FundamentalComponent struct {
	Name         string   `json:"name"`
	Weight       int      `json:"weight"`
	Availability string   `json:"availability"`
	Reason       string   `json:"reason,omitempty"`
	State        string   `json:"state,omitempty"`
	Value        string   `json:"value,omitempty"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type Fundamental struct {
	RuleVersion   string                 `json:"rule_version"`
	Availability  string                 `json:"availability"`
	Reason        string                 `json:"reason,omitempty"`
	Symbol        string                 `json:"symbol"`
	AsOf          string                 `json:"as_of,omitempty"`
	ReceiptNo     string                 `json:"receipt_no,omitempty"`
	PublishedAt   string                 `json:"published_at,omitempty"`
	PeriodEnd     string                 `json:"period_end,omitempty"`
	FactStatus    string                 `json:"fact_status,omitempty"`
	Value         string                 `json:"value,omitempty"`
	CoveragePct   int                    `json:"coverage_pct"`
	ConfidenceMax Confidence             `json:"confidence_max"`
	Components    []FundamentalComponent `json:"components"`
	EvidenceRefs  []string               `json:"evidence_refs"`
}

type FundamentalInput struct {
	Symbol, AsOf, ReceiptNo, PublishedAt, PeriodEnd              string
	EvidenceRefs                                                 []string
	AIDemandSalesDirection, AIDemandShipmentDirection            string
	DRAMASPDirection, NANDASPDirection                           string
	Revenue, PriorRevenue, OperatingProfit, PriorOperatingProfit string
	Inventory, PriorInventory, CapEx, PriorCapEx                 string
	OperatingCashFlow, PriorOperatingCashFlow                    string
	Cash, PriorCash, Borrowings, PriorBorrowings                 string
}

var fundamentalReceipt = regexp.MustCompile(`^[0-9]{14}$`)

func (f Fundamental) Component(name string) FundamentalComponent {
	for _, component := range f.Components {
		if component.Name == name {
			return component
		}
	}
	return FundamentalComponent{}
}

func UnavailableFundamental(symbol, reason string) Fundamental {
	specs := []struct {
		name   string
		weight int
	}{{FundamentalAIDemand, 2}, {FundamentalPricingCycle, 2}, {FundamentalSupplyDiscipline, 2}, {FundamentalEarnings, 2}, {FundamentalBalanceSheetCapEx, 1}, {FundamentalRegulatoryRisk, 1}}
	components := make([]FundamentalComponent, 0, len(specs))
	for _, spec := range specs {
		components = append(components, FundamentalComponent{Name: spec.name, Weight: spec.weight, Availability: AvailabilityUnavailable, Reason: reason, EvidenceRefs: []string{}})
	}
	components[len(components)-1].Reason = FundamentalReasonNoDeterministicRule
	return Fundamental{RuleVersion: FundamentalRuleVersion, Availability: AvailabilityUnavailable, Reason: reason, Symbol: symbol, ConfidenceMax: ConfidenceLow, Components: components, EvidenceRefs: []string{}}
}

func CalculateFundamental(input FundamentalInput) Fundamental {
	result := UnavailableFundamental(input.Symbol, FundamentalReasonInvalidInput)
	asOf, asErr := time.Parse(time.RFC3339Nano, input.AsOf)
	published, pubErr := time.Parse(time.RFC3339Nano, input.PublishedAt)
	period, periodErr := time.Parse("2006-01-02", input.PeriodEnd)
	if input.Symbol != "xyz:SKHY" || asErr != nil || pubErr != nil || periodErr != nil || published.After(asOf) || period.After(published) || !fundamentalReceipt.MatchString(input.ReceiptNo) || validateEvidenceRefs(input.EvidenceRefs) != nil {
		result.Reason = FundamentalReasonInsufficientCoverage
		return result
	}
	result.AsOf, result.PublishedAt, result.PeriodEnd, result.ReceiptNo = asOf.UTC().Format(time.RFC3339Nano), published.UTC().Format(time.RFC3339Nano), input.PeriodEnd, input.ReceiptNo
	result.FactStatus, result.EvidenceRefs = "REPORTED", append([]string(nil), input.EvidenceRefs...)
	states := []struct {
		name  string
		state string
	}{
		{FundamentalAIDemand, directionPairState(input.AIDemandSalesDirection, input.AIDemandShipmentDirection)},
		{FundamentalPricingCycle, directionPairState(input.DRAMASPDirection, input.NANDASPDirection)},
		{FundamentalSupplyDiscipline, ratioPairState(input.Inventory, input.Revenue, input.PriorInventory, input.PriorRevenue, input.CapEx, input.OperatingCashFlow, input.PriorCapEx, input.PriorOperatingCashFlow)},
		{FundamentalEarnings, earningsState(input.Revenue, input.PriorRevenue, input.OperatingProfit, input.PriorOperatingProfit)},
		{FundamentalBalanceSheetCapEx, balanceState(input.Cash, input.PriorCash, input.Borrowings, input.PriorBorrowings, input.CapEx, input.OperatingCashFlow)},
	}
	weights := map[string]int{FundamentalAIDemand: 2, FundamentalPricingCycle: 2, FundamentalSupplyDiscipline: 2, FundamentalEarnings: 2, FundamentalBalanceSheetCapEx: 1}
	earned, availableWeight := new(big.Rat), 0
	for index, candidate := range states {
		if candidate.state == "" {
			continue
		}
		fraction := fundamentalFraction(candidate.state)
		value := new(big.Rat).Mul(fraction, big.NewRat(int64(weights[candidate.name]), 1))
		result.Components[index] = FundamentalComponent{Name: candidate.name, Weight: weights[candidate.name], Availability: AvailabilityAvailable, State: candidate.state, Value: value.FloatString(2), EvidenceRefs: append([]string(nil), input.EvidenceRefs...)}
		earned.Add(earned, value)
		availableWeight += weights[candidate.name]
	}
	result.CoveragePct = availableWeight * 10
	if result.CoveragePct < 70 {
		result.Reason = FundamentalReasonInsufficientCoverage
		return result
	}
	result.Availability, result.Reason, result.ConfidenceMax = AvailabilityAvailable, "", ConfidenceMedium
	result.Value = new(big.Rat).Mul(earned, big.NewRat(10, int64(availableWeight))).FloatString(1)
	return result
}

func directionPairState(left, right string) string {
	if left == DirectionIncrease && right == DirectionIncrease {
		return "STRONG_POSITIVE"
	}
	if left == DirectionDecrease && right == DirectionDecrease {
		return "STRONG_NEGATIVE"
	}
	if (left == DirectionIncrease && right == DirectionDecrease) || (left == DirectionDecrease && right == DirectionIncrease) {
		return "NEUTRAL"
	}
	return ""
}

func ratioPairState(a, b, pa, pb, c, d, pc, pd string) string {
	first, ok1 := ratioImprovement(a, b, pa, pb)
	second, ok2 := ratioImprovement(c, d, pc, pd)
	if !ok1 || !ok2 {
		return ""
	}
	if first > 0 && second > 0 {
		return "STRONG_POSITIVE"
	}
	if first < 0 && second < 0 {
		return "STRONG_NEGATIVE"
	}
	if first >= 0 && second >= 0 {
		return "POSITIVE"
	}
	if first <= 0 && second <= 0 {
		return "NEGATIVE"
	}
	return "NEUTRAL"
}

func ratioImprovement(n, d, pn, pd string) (int, bool) {
	nv, e1 := parseDecimal(n)
	dv, e2 := parseDecimal(d)
	pnv, e3 := parseDecimal(pn)
	pdv, e4 := parseDecimal(pd)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || nv.Sign() < 0 || pnv.Sign() < 0 || dv.Sign() <= 0 || pdv.Sign() <= 0 {
		return 0, false
	}
	return -new(big.Rat).Quo(nv, dv).Cmp(new(big.Rat).Quo(pnv, pdv)), true
}

func earningsState(r, pr, op, pop string) string {
	rv, e1 := parseDecimal(r)
	prv, e2 := parseDecimal(pr)
	ov, e3 := parseDecimal(op)
	pov, e4 := parseDecimal(pop)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || rv.Sign() <= 0 || prv.Sign() <= 0 {
		return ""
	}
	revenueCmp := rv.Cmp(prv)
	marginCmp := new(big.Rat).Quo(ov, rv).Cmp(new(big.Rat).Quo(pov, prv))
	if revenueCmp > 0 && marginCmp > 0 {
		return "STRONG_POSITIVE"
	}
	if revenueCmp < 0 && marginCmp < 0 {
		return "STRONG_NEGATIVE"
	}
	if revenueCmp >= 0 && marginCmp >= 0 {
		return "POSITIVE"
	}
	if revenueCmp <= 0 && marginCmp <= 0 {
		return "NEGATIVE"
	}
	return "NEUTRAL"
}

func balanceState(cash, priorCash, debt, priorDebt, capex, ocf string) string {
	c, e1 := parseDecimal(cash)
	pc, e2 := parseDecimal(priorCash)
	d, e3 := parseDecimal(debt)
	pd, e4 := parseDecimal(priorDebt)
	cx, e5 := parseDecimal(capex)
	cf, e6 := parseDecimal(ocf)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || c.Sign() < 0 || pc.Sign() < 0 || d.Sign() < 0 || pd.Sign() < 0 || cx.Sign() < 0 || cf.Sign() <= 0 {
		return ""
	}
	net := new(big.Rat).Sub(c, d)
	priorNet := new(big.Rat).Sub(pc, pd)
	covered := cx.Cmp(cf) <= 0
	if net.Sign() >= 0 && net.Cmp(priorNet) > 0 && covered {
		return "STRONG_POSITIVE"
	}
	if net.Cmp(priorNet) > 0 && covered {
		return "POSITIVE"
	}
	if net.Cmp(priorNet) < 0 && !covered {
		return "STRONG_NEGATIVE"
	}
	if net.Cmp(priorNet) < 0 {
		return "NEGATIVE"
	}
	return "NEUTRAL"
}

func fundamentalFraction(state string) *big.Rat {
	switch state {
	case "STRONG_NEGATIVE":
		return big.NewRat(0, 1)
	case "NEGATIVE":
		return big.NewRat(1, 4)
	case "NEUTRAL":
		return big.NewRat(1, 2)
	case "POSITIVE":
		return big.NewRat(3, 4)
	default:
		return big.NewRat(1, 1)
	}
}
