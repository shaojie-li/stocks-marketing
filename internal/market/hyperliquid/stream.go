package hyperliquid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

const maxWebSocketMessageBytes = 2 << 20

func Stream(ctx context.Context, websocketURL string, symbols []string, handle func(WebSocketMessage) error, ready func()) error {
	connection, _, err := websocket.Dial(ctx, websocketURL, nil)
	if err != nil {
		return fmt.Errorf("connect Hyperliquid websocket: %w", err)
	}
	defer connection.CloseNow()
	connection.SetReadLimit(maxWebSocketMessageBytes)

	known := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		known[symbol] = struct{}{}
		for _, subscriptionType := range []string{"l2Book", "activeAssetCtx"} {
			request := struct {
				Method       string `json:"method"`
				Subscription struct {
					Type string `json:"type"`
					Coin string `json:"coin"`
				} `json:"subscription"`
			}{Method: "subscribe"}
			request.Subscription.Type = subscriptionType
			request.Subscription.Coin = symbol
			raw, err := json.Marshal(request)
			if err != nil {
				return err
			}
			if err := connection.Write(ctx, websocket.MessageText, raw); err != nil {
				return fmt.Errorf("subscribe %s %s: %w", subscriptionType, symbol, err)
			}
		}
	}

	expectedAcknowledgements := len(symbols) * 2
	acknowledged := make(map[string]struct{}, expectedAcknowledgements)
	readySignaled := false
	for {
		_, raw, err := connection.Read(ctx)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("read Hyperliquid websocket: %w", err)
		}
		message, err := ParseWebSocketMessage(raw, known)
		if err != nil {
			return err
		}
		if message.Kind == MessageSubscriptionAcknowledged {
			acknowledged[message.Symbol+"\x00"+message.Subscription] = struct{}{}
			if !readySignaled && len(acknowledged) == expectedAcknowledgements {
				readySignaled = true
				if ready != nil {
					ready()
				}
			}
		}
		if handle != nil {
			if err := handle(message); err != nil {
				return fmt.Errorf("handle Hyperliquid websocket message: %w", err)
			}
		}
	}
}
