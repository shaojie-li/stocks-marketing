package hyperliquid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestStreamSubscribesAndSignalsReadyOnlyAfterAllAcknowledgements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		for range 2 {
			_, raw, err := connection.Read(r.Context())
			if err != nil {
				t.Errorf("read subscription: %v", err)
				return
			}
			var request struct {
				Subscription struct {
					Type string `json:"type"`
					Coin string `json:"coin"`
				} `json:"subscription"`
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Errorf("decode subscription: %v", err)
				return
			}
			ack := `{"channel":"subscriptionResponse","data":{"method":"subscribe","subscription":{"type":"` + request.Subscription.Type + `","coin":"` + request.Subscription.Coin + `"}}}`
			if err := connection.Write(r.Context(), websocket.MessageText, []byte(ack)); err != nil {
				t.Errorf("write acknowledgement: %v", err)
				return
			}
		}
		messages := []string{
			`{"channel":"l2Book","data":{"coin":"xyz:SKHY","time":1786788513827,"levels":[[{"px":"166.18","sz":"7.82","n":3}],[{"px":"166.19","sz":"10.66","n":2}]]}}`,
			`{"channel":"activeAssetCtx","data":{"coin":"xyz:SKHY","ctx":{"funding":"0.0000001021","openInterest":"1399928.0799999998","prevDayPx":"167.42","oraclePx":"166.22","markPx":"166.18"}}}`,
		}
		for _, message := range messages {
			if err := connection.Write(r.Context(), websocket.MessageText, []byte(message)); err != nil {
				t.Errorf("write update: %v", err)
				return
			}
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ready atomic.Int32
	var updates atomic.Int32
	err := Stream(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), []string{"xyz:SKHY"}, func(message WebSocketMessage) error {
		if message.Kind == MessageBook || message.Kind == MessageAssetContext {
			if updates.Add(1) == 2 {
				cancel()
			}
		}
		return nil
	}, func() { ready.Add(1) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if ready.Load() != 1 || updates.Load() != 2 {
		t.Fatalf("ready/updates = %d/%d, want 1/2", ready.Load(), updates.Load())
	}
}
