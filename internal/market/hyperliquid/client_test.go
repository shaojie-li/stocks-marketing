package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientSnapshotUsesDynamicDiscoveryAndExactBooks(t *testing.T) {
	meta := readFixture(t, "t004-meta-and-asset-ctxs.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Type string `json:"type"`
			Coin string `json:"coin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Type {
		case "metaAndAssetCtxs":
			_, _ = w.Write(meta)
		case "perpsAtOpenInterestCap":
			_, _ = w.Write([]byte(`["xyz:SKHY"]`))
		case "l2Book":
			if request.Coin == "xyz:MU" {
				_, _ = w.Write([]byte(`{"coin":"xyz:MU","time":1786788513000,"levels":[[{"px":"973.18","sz":"0.308","n":1}],[{"px":"973.19","sz":"3.126","n":2}]]}`))
				return
			}
			_, _ = w.Write([]byte(`{"coin":"xyz:SKHY","time":1786788513827,"levels":[[{"px":"166.18","sz":"7.82","n":3}],[{"px":"166.19","sz":"10.66","n":2}]]}`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	receivedAt := time.UnixMilli(1786788514000)
	client := NewClient(server.Client(), server.URL, 2*time.Second)
	snapshots, err := client.Snapshot(context.Background(), []string{"xyz:MU", "xyz:SKHY"}, 7, receivedAt)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshots) != 2 || snapshots[0].Symbol != "xyz:MU" || snapshots[0].MarkPrice != "973.36" {
		t.Fatalf("metadata was not joined by exact symbol: %#v", snapshots)
	}
	if snapshots[0].ChangePercent != "-0.31134781" || snapshots[0].MarketStatus != "CONTINUOUS" || snapshots[0].Source != "hyperliquid" {
		t.Fatalf("quote audit fields are incomplete: %#v", snapshots[0])
	}
	if !snapshots[1].AtOpenInterestCap || snapshots[1].BestAsk != "166.19" || snapshots[1].Generation != 7 {
		t.Fatalf("book/cap/generation missing from SKHY: %#v", snapshots[1])
	}
}

func TestClientSnapshotFailsAtomicallyWhenRequiredAssetIsMissing(t *testing.T) {
	meta := readFixture(t, "t004-meta-and-asset-ctxs.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(meta) }))
	defer server.Close()
	client := NewClient(server.Client(), server.URL, time.Second)
	if snapshots, err := client.Snapshot(context.Background(), []string{"xyz:DOES_NOT_EXIST"}, 1, time.Now()); err == nil || snapshots != nil {
		t.Fatalf("missing asset should fail without partial output: %#v, %v", snapshots, err)
	}
}
