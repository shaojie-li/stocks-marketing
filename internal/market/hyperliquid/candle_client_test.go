package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientContract24HObservationsUsesIdenticalAlignedWindows(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 10, 25, 30, 0, time.UTC)
	bounds := Contract24HBounds(asOf)
	requested := make(map[string][2]int64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Type string `json:"type"`
			Req  struct {
				Coin      string `json:"coin"`
				Interval  string `json:"interval"`
				StartTime int64  `json:"startTime"`
				EndTime   int64  `json:"endTime"`
			} `json:"req"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Type == "metaAndAssetCtxs" {
			_, _ = w.Write(metaPayload("xyz:MU", "xyz:SMH"))
			return
		}
		if request.Type != "candleSnapshot" || request.Req.Interval != "1m" {
			t.Errorf("unexpected request: %#v", request)
		}
		requested[request.Req.Coin] = [2]int64{request.Req.StartTime, request.Req.EndTime}
		_ = json.NewEncoder(w).Encode(startCandlePayload(request.Req.Coin, bounds, "100"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	client.now = func() time.Time { return asOf }
	observations, err := client.Contract24HObservations(context.Background(), []string{"xyz:MU", "xyz:SMH"})
	if err != nil {
		t.Fatalf("contract 24h observations: %v", err)
	}
	if len(observations) != 2 || observations[0].ChangePct != "1.00000000" || observations[0].WindowEnd != asOf.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected observations: %#v", observations)
	}
	want := [2]int64{bounds.RequestStart.UnixMilli(), bounds.RequestEnd.UnixMilli()}
	for symbol, window := range requested {
		if window != want {
			t.Errorf("%s requested window = %v, want %v", symbol, window, want)
		}
	}
}

func TestClientContract24HObservationsFailsWithoutPartialResult(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 10, 25, 30, 0, time.UTC)
	bounds := Contract24HBounds(asOf)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Type string `json:"type"`
			Req  struct {
				Coin string `json:"coin"`
			} `json:"req"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Type == "metaAndAssetCtxs" {
			_, _ = w.Write(metaPayload("xyz:MU", "xyz:SMH"))
			return
		}
		if request.Req.Coin == "xyz:SMH" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_ = json.NewEncoder(w).Encode(startCandlePayload(request.Req.Coin, bounds, "100"))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 2*time.Second)
	client.now = func() time.Time { return asOf }
	observations, err := client.Contract24HObservations(context.Background(), []string{"xyz:MU", "xyz:SMH"})
	if err == nil || observations != nil {
		t.Fatalf("incomplete source returned partial observations: %#v, %v", observations, err)
	}
}

func startCandlePayload(symbol string, bounds WindowBounds, closePrice string) []map[string]any {
	return []map[string]any{{
		"t": bounds.RequestStart.UnixMilli(), "T": bounds.StartClose.UnixMilli(),
		"s": symbol, "i": "1m", "o": closePrice, "c": closePrice,
		"h": closePrice, "l": closePrice, "v": "1", "n": 1,
	}}
}

func metaPayload(symbols ...string) []byte {
	universe := make([]map[string]any, len(symbols))
	contexts := make([]map[string]any, len(symbols))
	for index, symbol := range symbols {
		universe[index] = map[string]any{"name": symbol, "szDecimals": 3}
		contexts[index] = map[string]any{
			"funding": "0", "openInterest": "1", "prevDayPx": "100",
			"dayNtlVlm": "1", "oraclePx": "101", "markPx": "101", "midPx": "101",
		}
	}
	raw, _ := json.Marshal([]any{map[string]any{"universe": universe}, contexts})
	return raw
}
