package hyperliquid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
)

var decimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

type AssetContext struct {
	Symbol           string
	MarkPrice        string
	OraclePrice      string
	OpenInterest     string
	Funding          string
	PreviousDayPrice string
	ChangePercent    string
	Delisted         bool
}

type Level struct {
	Price string
	Size  string
	Count int
}

type Book struct {
	Symbol     string
	TimeMillis int64
	BestBid    Level
	BestAsk    Level
}

type MessageKind string

const (
	MessageSubscriptionAcknowledged MessageKind = "SUBSCRIPTION_ACKNOWLEDGED"
	MessageBook                     MessageKind = "BOOK"
	MessageAssetContext             MessageKind = "ASSET_CONTEXT"
)

type WebSocketMessage struct {
	Kind         MessageKind
	Symbol       string
	Subscription string
	Book         *Book
	Context      *AssetContext
}

func ParseMetaAndAssetContexts(raw []byte) ([]AssetContext, error) {
	var payload []json.RawMessage
	if err := decodeOne(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode metaAndAssetCtxs: %w", err)
	}
	if len(payload) != 2 {
		return nil, errors.New("metaAndAssetCtxs must contain metadata and contexts")
	}
	var metadata struct {
		Universe []struct {
			Name       string `json:"name"`
			IsDelisted bool   `json:"isDelisted"`
		} `json:"universe"`
	}
	var contexts []struct {
		MarkPrice        *string `json:"markPx"`
		OraclePrice      *string `json:"oraclePx"`
		OpenInterest     *string `json:"openInterest"`
		Funding          *string `json:"funding"`
		PreviousDayPrice *string `json:"prevDayPx"`
	}
	if err := json.Unmarshal(payload[0], &metadata); err != nil {
		return nil, fmt.Errorf("decode universe: %w", err)
	}
	if err := json.Unmarshal(payload[1], &contexts); err != nil {
		return nil, fmt.Errorf("decode contexts: %w", err)
	}
	if len(metadata.Universe) == 0 || len(metadata.Universe) != len(contexts) {
		return nil, errors.New("universe/context length mismatch")
	}
	seen := make(map[string]struct{}, len(metadata.Universe))
	assets := make([]AssetContext, len(metadata.Universe))
	for i, item := range metadata.Universe {
		if item.Name == "" {
			return nil, fmt.Errorf("universe index %d has empty symbol", i)
		}
		if _, exists := seen[item.Name]; exists {
			return nil, fmt.Errorf("duplicate symbol %q", item.Name)
		}
		seen[item.Name] = struct{}{}
		context := contexts[i]
		if context.MarkPrice == nil || context.OraclePrice == nil || context.OpenInterest == nil || context.Funding == nil || context.PreviousDayPrice == nil {
			return nil, fmt.Errorf("asset %q has missing context field", item.Name)
		}
		for field, value := range map[string]string{
			"markPx": *context.MarkPrice, "oraclePx": *context.OraclePrice,
			"openInterest": *context.OpenInterest, "funding": *context.Funding,
			"prevDayPx": *context.PreviousDayPrice,
		} {
			if !decimal(value) {
				return nil, fmt.Errorf("asset %q has invalid %s", item.Name, field)
			}
		}
		changePercent, err := percentChange(*context.MarkPrice, *context.PreviousDayPrice)
		if err != nil {
			return nil, fmt.Errorf("asset %q has invalid change percent inputs", item.Name)
		}
		assets[i] = AssetContext{
			Symbol: item.Name, MarkPrice: *context.MarkPrice, OraclePrice: *context.OraclePrice,
			OpenInterest: *context.OpenInterest, Funding: *context.Funding, Delisted: item.IsDelisted,
			PreviousDayPrice: *context.PreviousDayPrice, ChangePercent: changePercent,
		}
	}
	return assets, nil
}

func ParseBook(raw []byte, expectedSymbol string) (Book, error) {
	var payload struct {
		Coin   string `json:"coin"`
		Time   int64  `json:"time"`
		Levels [][]struct {
			Price string `json:"px"`
			Size  string `json:"sz"`
			Count int    `json:"n"`
		} `json:"levels"`
	}
	if err := decodeOne(raw, &payload); err != nil {
		return Book{}, fmt.Errorf("decode l2Book: %w", err)
	}
	if payload.Coin == "" || payload.Coin != expectedSymbol {
		return Book{}, fmt.Errorf("book symbol %q does not match %q", payload.Coin, expectedSymbol)
	}
	if payload.Time <= 0 || len(payload.Levels) != 2 || len(payload.Levels[0]) == 0 || len(payload.Levels[1]) == 0 {
		return Book{}, errors.New("book is missing timestamp or one side")
	}
	bestBid, bestAsk := payload.Levels[0][0], payload.Levels[1][0]
	if !positiveDecimal(bestBid.Price) || !positiveDecimal(bestBid.Size) || !positiveDecimal(bestAsk.Price) || !positiveDecimal(bestAsk.Size) {
		return Book{}, errors.New("book contains invalid best level")
	}
	return Book{
		Symbol: payload.Coin, TimeMillis: payload.Time,
		BestBid: Level{Price: bestBid.Price, Size: bestBid.Size, Count: bestBid.Count},
		BestAsk: Level{Price: bestAsk.Price, Size: bestAsk.Size, Count: bestAsk.Count},
	}, nil
}

func ParseWebSocketMessage(raw []byte, known map[string]struct{}) (WebSocketMessage, error) {
	var envelope struct {
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := decodeOne(raw, &envelope); err != nil {
		return WebSocketMessage{}, fmt.Errorf("decode websocket envelope: %w", err)
	}
	switch envelope.Channel {
	case "subscriptionResponse":
		var data struct {
			Method       string `json:"method"`
			Subscription struct {
				Type string `json:"type"`
				Coin string `json:"coin"`
			} `json:"subscription"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil || data.Method != "subscribe" {
			return WebSocketMessage{}, errors.New("invalid subscription acknowledgement")
		}
		if _, ok := known[data.Subscription.Coin]; !ok {
			return WebSocketMessage{}, fmt.Errorf("acknowledgement for unknown asset %q", data.Subscription.Coin)
		}
		if data.Subscription.Type != "l2Book" && data.Subscription.Type != "activeAssetCtx" {
			return WebSocketMessage{}, fmt.Errorf("unexpected subscription type %q", data.Subscription.Type)
		}
		return WebSocketMessage{Kind: MessageSubscriptionAcknowledged, Symbol: data.Subscription.Coin, Subscription: data.Subscription.Type}, nil
	case "l2Book":
		var symbol struct {
			Coin string `json:"coin"`
		}
		if err := json.Unmarshal(envelope.Data, &symbol); err != nil {
			return WebSocketMessage{}, err
		}
		if _, ok := known[symbol.Coin]; !ok {
			return WebSocketMessage{}, fmt.Errorf("book for unknown asset %q", symbol.Coin)
		}
		book, err := ParseBook(envelope.Data, symbol.Coin)
		if err != nil {
			return WebSocketMessage{}, err
		}
		return WebSocketMessage{Kind: MessageBook, Symbol: symbol.Coin, Book: &book}, nil
	case "activeAssetCtx":
		var data struct {
			Coin string `json:"coin"`
			Ctx  struct {
				MarkPrice        *string `json:"markPx"`
				OraclePrice      *string `json:"oraclePx"`
				OpenInterest     *string `json:"openInterest"`
				Funding          *string `json:"funding"`
				PreviousDayPrice *string `json:"prevDayPx"`
			} `json:"ctx"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return WebSocketMessage{}, err
		}
		if _, ok := known[data.Coin]; !ok {
			return WebSocketMessage{}, fmt.Errorf("context for unknown asset %q", data.Coin)
		}
		if data.Ctx.MarkPrice == nil || data.Ctx.OraclePrice == nil || data.Ctx.OpenInterest == nil || data.Ctx.Funding == nil || data.Ctx.PreviousDayPrice == nil {
			return WebSocketMessage{}, errors.New("asset context has missing field")
		}
		for _, value := range []string{*data.Ctx.MarkPrice, *data.Ctx.OraclePrice, *data.Ctx.OpenInterest, *data.Ctx.Funding, *data.Ctx.PreviousDayPrice} {
			if !decimal(value) {
				return WebSocketMessage{}, errors.New("asset context has invalid decimal")
			}
		}
		changePercent, err := percentChange(*data.Ctx.MarkPrice, *data.Ctx.PreviousDayPrice)
		if err != nil {
			return WebSocketMessage{}, err
		}
		context := AssetContext{Symbol: data.Coin, MarkPrice: *data.Ctx.MarkPrice, OraclePrice: *data.Ctx.OraclePrice, OpenInterest: *data.Ctx.OpenInterest, Funding: *data.Ctx.Funding, PreviousDayPrice: *data.Ctx.PreviousDayPrice, ChangePercent: changePercent}
		return WebSocketMessage{Kind: MessageAssetContext, Symbol: data.Coin, Context: &context}, nil
	default:
		return WebSocketMessage{}, fmt.Errorf("unexpected websocket channel %q", envelope.Channel)
	}
}

func decodeOne(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func decimal(value string) bool {
	if !decimalPattern.MatchString(value) {
		return false
	}
	_, ok := new(big.Rat).SetString(value)
	return ok
}

func positiveDecimal(value string) bool {
	if !decimalPattern.MatchString(value) {
		return false
	}
	n, ok := new(big.Rat).SetString(value)
	return ok && n.Sign() > 0
}

func percentChange(current, previous string) (string, error) {
	currentValue, currentOK := new(big.Rat).SetString(current)
	previousValue, previousOK := new(big.Rat).SetString(previous)
	if !currentOK || !previousOK || previousValue.Sign() == 0 {
		return "", errors.New("invalid percent change input")
	}
	change := new(big.Rat).Sub(currentValue, previousValue)
	change.Quo(change, previousValue)
	change.Mul(change, big.NewRat(100, 1))
	return change.FloatString(8), nil
}
