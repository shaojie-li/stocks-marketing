package domain

import "testing"

func TestEvaluateBearishCatalystClassifiesExactBoundaries(t *testing.T) {
	tests := map[string]struct {
		targetEnd    string
		benchmarkEnd string
		want         CatalystState
	}{
		"accepted equality": {"99.50", "99.75", CatalystAccepted},
		"neutral":           {"99.51", "99.75", CatalystNeutral},
		"rejected equality": {"100.50", "100.25", CatalystRejected},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			window := finalCatalystWindow()
			window.TargetEndPrice = test.targetEnd
			window.BenchmarkEndPrice = test.benchmarkEnd
			result, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-15T08:44:00Z", window)
			if err != nil {
				t.Fatal(err)
			}
			if result.State != test.want || result.Preliminary {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestEvaluateCatalystMarksIncompleteWindowPreliminary(t *testing.T) {
	window := CatalystPriceWindow{
		TargetSymbol: "xyz:SKHY", BenchmarkSymbol: "xyz:SMSN",
		WindowStart: "2026-08-14T07:43:59.999Z", WindowEnd: "2026-08-14T08:43:59.999Z",
		TargetStartPrice: "100", TargetEndPrice: "99", BenchmarkStartPrice: "100", BenchmarkEndPrice: "99.5",
		EvidenceRefs: []string{"ev-price-skhy", "ev-price-smsn"},
	}
	result, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-14T08:44:00Z", window)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preliminary || result.State != CatalystAccepted || result.DirectionalReturnPct != "1.00000000" || result.DirectionalAlphaPP != "0.50000000" {
		t.Fatalf("preliminary result = %#v", result)
	}
}

func TestEvaluateCatalystRejectsMismatchedOrMissingPriceEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*CatalystPriceWindow){
		"wrong start": func(window *CatalystPriceWindow) { window.WindowStart = "2026-08-14T07:44:59.999Z" },
		"wrong end":   func(window *CatalystPriceWindow) { window.WindowEnd = "2026-08-15T07:44:59.999Z" },
		"no evidence": func(window *CatalystPriceWindow) { window.EvidenceRefs = nil },
		"zero price":  func(window *CatalystPriceWindow) { window.TargetStartPrice = "0" },
	} {
		t.Run(name, func(t *testing.T) {
			window := finalCatalystWindow()
			mutate(&window)
			if _, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-15T08:44:00Z", window); err == nil {
				t.Fatal("invalid Catalyst input was accepted")
			}
		})
	}
}

func TestEvaluateCatalystRejectsDirectionOrBenchmarkOutsideFrozenSlice(t *testing.T) {
	event := testCatalystEvent()
	event.ExpectedDirection = DirectionBullish
	if _, err := EvaluateCatalyst(event, "2026-08-15T08:44:00Z", finalCatalystWindow()); err == nil {
		t.Fatal("bullish direction was accepted for the loss disclosure")
	}
	window := finalCatalystWindow()
	window.BenchmarkSymbol = "xyz:KR200"
	if _, err := EvaluateCatalyst(testCatalystEvent(), "2026-08-15T08:44:00Z", window); err == nil {
		t.Fatal("benchmark outside the frozen slice was accepted")
	}
}

func testCatalystEvent() CatalystEvent {
	return CatalystEvent{
		SourceEventID: "20260814802986", PublishedAt: "2026-08-14T07:44:00Z", EventAt: "2026-08-14T07:44:00Z",
		Source: "dart", SourceTier: "OFFICIAL", OriginalSource: "https://dart.fss.or.kr/api/link.jsp?rcpNo=20260814802986",
		Category: CatalystCategoryDerivativeTradingLoss, AffectedAssets: []string{"xyz:SKHY"}, Importance: "HIGH",
		FactStatus: "CONFIRMED", ExpectedDirection: DirectionBearish, EvidenceRefs: []string{"ev-dart", "ev-filing"},
	}
}

func finalCatalystWindow() CatalystPriceWindow {
	return CatalystPriceWindow{
		TargetSymbol: "xyz:SKHY", BenchmarkSymbol: "xyz:SMSN",
		WindowStart: "2026-08-14T07:43:59.999Z", WindowEnd: "2026-08-15T07:43:59.999Z",
		TargetStartPrice: "100", TargetEndPrice: "98", BenchmarkStartPrice: "100", BenchmarkEndPrice: "99",
		EvidenceRefs: []string{"ev-price-skhy", "ev-price-smsn"},
	}
}
