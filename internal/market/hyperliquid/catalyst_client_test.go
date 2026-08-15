package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCatalystPriceWindowUsesCompleteAlignedCandles(t *testing.T) {
	eventAt := time.Date(2026, 8, 14, 7, 44, 0, 0, time.UTC)
	asOf := eventAt.Add(25 * time.Hour)
	bounds, err := CatalystWindowBounds(eventAt, asOf)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Req struct {
				Coin      string `json:"coin"`
				Interval  string `json:"interval"`
				StartTime int64  `json:"startTime"`
				EndTime   int64  `json:"endTime"`
			} `json:"req"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Req.Interval != "1m" || request.Req.StartTime != bounds.RequestStart.UnixMilli() || request.Req.EndTime != bounds.WindowEnd.UnixMilli() {
			t.Errorf("request = %#v", request)
		}
		start, end := "100", "98"
		if request.Req.Coin == "xyz:SMSN" {
			end = "99"
		}
		_ = json.NewEncoder(w).Encode(catalystCandles(request.Req.Coin, bounds, start, end))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	window, err := client.CatalystPriceWindow(context.Background(), eventAt, asOf, "xyz:SKHY", "xyz:SMSN")
	if err != nil {
		t.Fatal(err)
	}
	if window.WindowStart != bounds.WindowStart.Format(time.RFC3339Nano) || window.WindowEnd != bounds.WindowEnd.Format(time.RFC3339Nano) || window.TargetStartPrice != "100" || window.TargetEndPrice != "98" || window.BenchmarkEndPrice != "99" || len(window.EvidenceRefs) != 2 {
		t.Fatalf("window = %#v", window)
	}
}

func TestClientCatalystPriceWindowRetriesTransientHTTPFailure(t *testing.T) {
	eventAt := time.Date(2026, 8, 14, 7, 44, 0, 0, time.UTC)
	asOf := eventAt.Add(time.Hour)
	bounds, _ := CatalystWindowBounds(eventAt, asOf)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var request struct {
			Req struct {
				Coin string `json:"coin"`
			} `json:"req"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		_ = json.NewEncoder(w).Encode(catalystCandles(request.Req.Coin, bounds, "100", "99"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	client.retryDelay = func(int) time.Duration { return 0 }
	if _, err := client.CatalystPriceWindow(context.Background(), eventAt, asOf, "xyz:SKHY", "xyz:SMSN"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestClientCatalystPriceWindowFailsWithoutPartialPair(t *testing.T) {
	eventAt := time.Date(2026, 8, 14, 7, 44, 0, 0, time.UTC)
	asOf := eventAt.Add(time.Hour)
	bounds, _ := CatalystWindowBounds(eventAt, asOf)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Req struct {
				Coin string `json:"coin"`
			} `json:"req"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Req.Coin == "xyz:SMSN" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(catalystCandles(request.Req.Coin, bounds, "100", "99"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	if window, err := client.CatalystPriceWindow(context.Background(), eventAt, asOf, "xyz:SKHY", "xyz:SMSN"); err == nil || window.TargetSymbol != "" {
		t.Fatalf("partial window returned: %#v, %v", window, err)
	}
}

func catalystCandles(symbol string, bounds CatalystBounds, startPrice, endPrice string) []map[string]any {
	count := int(bounds.WindowEnd.Sub(bounds.RequestStart)/time.Minute) + 1
	items := make([]map[string]any, count)
	for index := range items {
		openTime := bounds.RequestStart.Add(time.Duration(index) * time.Minute)
		price := startPrice
		if index == count-1 {
			price = endPrice
		}
		items[index] = map[string]any{
			"t": openTime.UnixMilli(), "T": openTime.Add(time.Minute - time.Millisecond).UnixMilli(),
			"s": symbol, "i": "1m", "o": price, "c": price, "h": price, "l": price, "v": "1", "n": 1,
		}
	}
	return items
}
