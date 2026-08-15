package market

import (
	"testing"
	"time"
)

func TestEligibilityGateSeparatesAnalysisAndEntry(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:SKHY"}, 5*time.Second)
	state.Replace(Snapshot{
		Symbol: "xyz:SKHY", MarkPrice: "166.18", OraclePrice: "166.22", OpenInterest: "1399928.0799999998",
		BestBid: "166.18", BestAsk: "166.19", ExchangeTime: now.Add(-time.Second), ReceivedAt: now.Add(-500 * time.Millisecond),
		AtOpenInterestCap: true, Generation: 1,
	})
	result := state.Eligibility("xyz:SKHY", now)
	if !result.AnalysisEligible {
		t.Fatalf("analysis should remain eligible: %#v", result)
	}
	if result.EntryEligible || !contains(result.EntryReasons, ReasonOpenInterestCap) {
		t.Fatalf("new entry should be blocked by OI cap: %#v", result)
	}
}

func TestEligibilityGateClosesForStaleAndRecoveryState(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:SKHY"}, 5*time.Second)
	state.Replace(Snapshot{
		Symbol: "xyz:SKHY", MarkPrice: "166.18", OraclePrice: "166.22", OpenInterest: "1",
		BestBid: "166.18", BestAsk: "166.19", ExchangeTime: now.Add(-6 * time.Second), ReceivedAt: now, Generation: 1,
	})
	if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible || !contains(got.AnalysisReasons, ReasonStale) {
		t.Fatalf("stale data should close gate: %#v", got)
	}
	state.Replace(Snapshot{
		Symbol: "xyz:SKHY", MarkPrice: "166.18", OraclePrice: "166.22", OpenInterest: "1",
		BestBid: "166.18", BestAsk: "166.19", ExchangeTime: now, ReceivedAt: now, Generation: 2,
	})
	state.BeginRecovery()
	if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible || !contains(got.AnalysisReasons, ReasonRecoveryPending) {
		t.Fatalf("recovery should close gate: %#v", got)
	}
	state.CompleteRecovery(2)
	if got := state.Eligibility("xyz:SKHY", now); !got.AnalysisEligible {
		t.Fatalf("fresh synchronized generation should reopen gate: %#v", got)
	}
}

func TestStateRejectsTimestampRegression(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:SKHY"}, 5*time.Second)
	if !state.Replace(Snapshot{Symbol: "xyz:SKHY", MarkPrice: "2", OraclePrice: "2", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now, ReceivedAt: now, Generation: 2}) {
		t.Fatal("initial snapshot rejected")
	}
	if state.Replace(Snapshot{Symbol: "xyz:SKHY", MarkPrice: "1", OraclePrice: "1", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now.Add(-time.Second), ReceivedAt: now, Generation: 1}) {
		t.Fatal("older generation/timestamp replaced current data")
	}
	got, ok := state.Snapshot("xyz:SKHY")
	if !ok || got.MarkPrice != "2" {
		t.Fatalf("current state regressed: %#v, %v", got, ok)
	}
}

func TestReplaceBatchIsAllOrNothing(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:MU", "xyz:SKHY"}, 5*time.Second)
	valid := Snapshot{Symbol: "xyz:MU", MarkPrice: "2", OraclePrice: "2", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now, ReceivedAt: now, Generation: 1}
	invalid := Snapshot{Symbol: "xyz:OTHER", MarkPrice: "2", OraclePrice: "2", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now, ReceivedAt: now, Generation: 1}
	if state.ReplaceBatch([]Snapshot{valid, invalid}) {
		t.Fatal("batch containing unknown asset was accepted")
	}
	if _, ok := state.Snapshot("xyz:MU"); ok {
		t.Fatal("partial batch update leaked into state")
	}
	if state.ReplaceBatch([]Snapshot{valid}) {
		t.Fatal("batch missing a watched asset was accepted")
	}
}

func TestEligibilityRejectsStaleReceiveTimeAndImplausibleFutureTimestamp(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:SKHY"}, 5*time.Second)
	base := Snapshot{Symbol: "xyz:SKHY", MarkPrice: "2", OraclePrice: "2", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now, ReceivedAt: now.Add(-6 * time.Second), Generation: 1}
	state.Replace(base)
	if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible || !contains(got.AnalysisReasons, ReasonStale) {
		t.Fatalf("stale local receive time was accepted: %#v", got)
	}
	base.Generation = 2
	base.ReceivedAt = now
	base.ExchangeTime = now.Add(6 * time.Second)
	state.Replace(base)
	if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible || !contains(got.AnalysisReasons, ReasonStale) {
		t.Fatalf("implausible future exchange time was accepted: %#v", got)
	}
}

func TestEligibilityRejectsNonDecimalNumericSyntax(t *testing.T) {
	now := time.UnixMilli(1786788514000)
	state := NewState([]string{"xyz:SKHY"}, 5*time.Second)
	state.Replace(Snapshot{Symbol: "xyz:SKHY", MarkPrice: "1/2", OraclePrice: "2", OpenInterest: "1", BestBid: "1", BestAsk: "2", ExchangeTime: now, ReceivedAt: now, Generation: 1})
	if got := state.Eligibility("xyz:SKHY", now); got.AnalysisEligible || !contains(got.AnalysisReasons, ReasonMissingPrice) {
		t.Fatalf("fraction syntax was accepted as exchange decimal: %#v", got)
	}
}

func contains(values []Reason, target Reason) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
