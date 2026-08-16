package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

func TestClientSKHYCrowdingPaginatesFundingAndCombinesOfficialInputs(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	start := asOf.Truncate(time.Hour).Add(-719 * time.Hour)
	fundingRequests := make([]int64, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Type      string `json:"type"`
			Coin      string `json:"coin"`
			StartTime int64  `json:"startTime"`
			EndTime   int64  `json:"endTime"`
			Req       struct {
				Coin     string `json:"coin"`
				Interval string `json:"interval"`
			} `json:"req"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.Type {
		case "metaAndAssetCtxs":
			_, _ = w.Write(metaPayload("xyz:SKHY"))
		case "fundingHistory":
			fundingRequests = append(fundingRequests, request.StartTime)
			offset, count := 0, 500
			if len(fundingRequests) == 2 {
				offset, count = 500, 220
			}
			_ = json.NewEncoder(w).Encode(fundingPayload(start, offset, count))
		case "candleSnapshot":
			if request.Req.Coin != "xyz:SKHY" || request.Req.Interval != "1d" {
				t.Errorf("unexpected candle request: %#v", request)
			}
			_ = json.NewEncoder(w).Encode(dailyCandlePayload(asOf, 50))
		default:
			t.Errorf("unexpected request type %q", request.Type)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	result, err := client.SKHYCrowding(context.Background(), "xyz:SKHY", asOf)
	if err != nil {
		t.Fatalf("load Crowding: %v", err)
	}
	if result.Availability != domain.AvailabilityAvailable || result.AvailableInputs != 4 || len(result.EvidenceRefs) != 4 {
		t.Fatalf("Crowding = %#v", result)
	}
	if len(fundingRequests) != 2 || fundingRequests[0] != start.UnixMilli() || fundingRequests[1] != start.Add(499*time.Hour).Add(100*time.Millisecond).UnixMilli()+1 {
		t.Fatalf("funding pagination = %v", fundingRequests)
	}
	oi := result.Component(domain.CrowdingInputOpenInterest)
	if oi.Availability != domain.AvailabilityUnavailable || oi.CurrentValue != "1.00000000" || oi.Reason != domain.CrowdingReasonHistoricalOIUnavailable {
		t.Fatalf("current OI was not kept separate from historical availability: %#v", oi)
	}
}

func TestClientSKHYCrowdingDegradesOnlyFailedFundingSource(t *testing.T) {
	asOf := time.Date(2026, 8, 20, 12, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Type string `json:"type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch request.Type {
		case "metaAndAssetCtxs":
			_, _ = w.Write(metaPayload("xyz:SKHY"))
		case "fundingHistory":
			http.Error(w, "unavailable", http.StatusBadRequest)
		case "candleSnapshot":
			_ = json.NewEncoder(w).Encode(dailyCandlePayload(asOf, 50))
		}
	}))
	defer server.Close()

	result, err := NewClient(server.Client(), server.URL, 2*time.Second).SKHYCrowding(context.Background(), "xyz:SKHY", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if result.Component(domain.CrowdingInputFunding).Reason != domain.CrowdingReasonSourceError || result.Component(domain.CrowdingInputPriceExtension).Availability != domain.AvailabilityAvailable {
		t.Fatalf("source failure erased independent daily evidence: %#v", result.Components)
	}
}

func fundingPayload(start time.Time, offset, count int) []map[string]any {
	result := make([]map[string]any, count)
	for index := range result {
		at := start.Add(time.Duration(offset+index) * time.Hour).Add(100 * time.Millisecond)
		result[index] = map[string]any{"coin": "xyz:SKHY", "fundingRate": "0.0001", "premium": "0.0001", "time": at.UnixMilli()}
	}
	return result
}

func dailyCandlePayload(asOf time.Time, completed int) []map[string]any {
	start := asOf.Truncate(24*time.Hour).AddDate(0, 0, -completed)
	result := make([]map[string]any, completed+1)
	for index := range result {
		openTime := start.AddDate(0, 0, index)
		price := 100 + index
		volume := "100"
		if index == completed-1 {
			volume = "200"
		}
		result[index] = map[string]any{
			"t": openTime.UnixMilli(), "T": openTime.Add(24*time.Hour - time.Millisecond).UnixMilli(), "s": "xyz:SKHY", "i": "1d",
			"o": strconv.Itoa(price), "c": strconv.Itoa(price), "h": strconv.Itoa(price + 2), "l": strconv.Itoa(price - 2), "v": volume, "n": 1,
		}
	}
	return result
}
