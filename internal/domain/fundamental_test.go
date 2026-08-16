package domain

import "testing"

func TestCalculateFundamentalUsesOnlyReportedOperatingFacts(t *testing.T) {
	result := CalculateFundamental(FundamentalInput{
		Symbol: "xyz:SKHY", AsOf: "2026-08-16T00:00:00Z", ReceiptNo: "20260814003509",
		PublishedAt: "2026-08-14T00:00:00Z", PeriodEnd: "2026-06-30", EvidenceRefs: []string{"opendart:20260814003509"},
		AIDemandSalesDirection: DirectionIncrease, AIDemandShipmentDirection: DirectionIncrease,
		DRAMASPDirection: DirectionIncrease, NANDASPDirection: DirectionIncrease,
		Revenue: "131895033000000", PriorRevenue: "39871093000000", OperatingProfit: "98152891000000", PriorOperatingProfit: "16653355000000",
		Inventory: "17985706000000", PriorInventory: "13408333000000", CapEx: "18328836000000", PriorCapEx: "10615739000000",
		OperatingCashFlow: "91742501000000", PriorOperatingCashFlow: "18189841000000",
		Cash: "26835986000000", PriorCash: "14923766000000", Borrowings: "18586634000000", PriorBorrowings: "22247905000000",
	})
	if result.Availability != AvailabilityAvailable || result.CoveragePct != 90 || result.Value != "10.0" || result.FactStatus != "REPORTED" {
		t.Fatalf("Fundamental = %#v", result)
	}
	if component := result.Component(FundamentalRegulatoryRisk); component.Availability != AvailabilityUnavailable || component.Reason != FundamentalReasonNoDeterministicRule {
		t.Fatalf("regulatory component = %#v", component)
	}
}

func TestCalculateFundamentalDoesNotFillMissingFacts(t *testing.T) {
	result := CalculateFundamental(FundamentalInput{Symbol: "xyz:SKHY", AsOf: "2026-08-16T00:00:00Z"})
	if result.Availability != AvailabilityUnavailable || result.Value != "" || result.CoveragePct != 0 || result.Reason != FundamentalReasonInsufficientCoverage {
		t.Fatalf("Fundamental = %#v", result)
	}
}

func TestCalculateFundamentalRejectsNonPositiveDenominators(t *testing.T) {
	input := validFundamentalInput()
	input.OperatingCashFlow = "0"
	result := CalculateFundamental(input)
	if result.Component(FundamentalSupplyDiscipline).Availability != AvailabilityUnavailable || result.Component(FundamentalBalanceSheetCapEx).Availability != AvailabilityUnavailable {
		t.Fatalf("invalid cash-flow denominator was accepted: %#v", result)
	}
}

func validFundamentalInput() FundamentalInput {
	return FundamentalInput{
		Symbol: "xyz:SKHY", AsOf: "2026-08-16T00:00:00Z", ReceiptNo: "20260814003509", PublishedAt: "2026-08-14T00:00:00Z", PeriodEnd: "2026-06-30",
		EvidenceRefs: []string{"opendart:20260814003509"}, AIDemandSalesDirection: DirectionIncrease, AIDemandShipmentDirection: DirectionIncrease,
		DRAMASPDirection: DirectionIncrease, NANDASPDirection: DirectionIncrease, Revenue: "100", PriorRevenue: "80", OperatingProfit: "30", PriorOperatingProfit: "16",
		Inventory: "10", PriorInventory: "12", CapEx: "10", PriorCapEx: "12", OperatingCashFlow: "30", PriorOperatingCashFlow: "20",
		Cash: "30", PriorCash: "20", Borrowings: "10", PriorBorrowings: "15",
	}
}
