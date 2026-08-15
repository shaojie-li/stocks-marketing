package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/market"
)

const maxResponseBytes = 4 << 20

type Client struct {
	httpClient *http.Client
	infoURL    string
	timeout    time.Duration
	now        func() time.Time
}

func NewClient(httpClient *http.Client, infoURL string, timeout time.Duration) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient, infoURL: infoURL, timeout: timeout, now: time.Now}
}

func (c *Client) Contract24HObservations(ctx context.Context, symbols []string) ([]domain.Observation, error) {
	if len(symbols) == 0 {
		return nil, errors.New("no symbols configured for CONTRACT_24H")
	}
	var contextRaw json.RawMessage
	if err := c.post(ctx, map[string]any{"type": "metaAndAssetCtxs", "dex": dexFor(symbols)}, &contextRaw); err != nil {
		return nil, fmt.Errorf("load current asset contexts: %w", err)
	}
	capturedAt := c.now().UTC()
	contexts, err := ParseMetaAndAssetContexts(contextRaw)
	if err != nil {
		return nil, err
	}
	bySymbol := make(map[string]AssetContext, len(contexts))
	for _, asset := range contexts {
		bySymbol[asset.Symbol] = asset
	}
	for _, symbol := range symbols {
		asset, ok := bySymbol[symbol]
		if !ok || asset.Delisted || !positiveDecimal(asset.MarkPrice) || !positiveDecimal(asset.OraclePrice) || !positiveDecimal(asset.OpenInterest) {
			return nil, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: current mark unavailable for %s", symbol)
		}
	}
	bounds := Contract24HBounds(capturedAt)
	observations := make([]domain.Observation, 0, len(symbols))
	for _, symbol := range symbols {
		var raw json.RawMessage
		payload := map[string]any{
			"type": "candleSnapshot",
			"req": map[string]any{
				"coin": symbol, "interval": "1m",
				"startTime": bounds.RequestStart.UnixMilli(), "endTime": bounds.RequestEnd.UnixMilli(),
			},
		}
		if err := c.post(ctx, payload, &raw); err != nil {
			return nil, fmt.Errorf("load CONTRACT_24H candles for %s: %w", symbol, err)
		}
		candles, err := ParseCandles(raw, symbol, "1m")
		if err != nil {
			return nil, fmt.Errorf("load CONTRACT_24H candles for %s: %w", symbol, err)
		}
		observation, err := BuildContract24HObservation(candles, bounds, bySymbol[symbol].MarkPrice)
		if err != nil {
			return nil, fmt.Errorf("build CONTRACT_24H observation for %s: %w", symbol, err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func (c *Client) Snapshot(ctx context.Context, symbols []string, generation uint64, receivedAt time.Time) ([]market.Snapshot, error) {
	var raw json.RawMessage
	if err := c.post(ctx, map[string]any{"type": "metaAndAssetCtxs", "dex": dexFor(symbols)}, &raw); err != nil {
		return nil, err
	}
	contexts, err := ParseMetaAndAssetContexts(raw)
	if err != nil {
		return nil, err
	}
	bySymbol := make(map[string]AssetContext, len(contexts))
	for _, asset := range contexts {
		bySymbol[asset.Symbol] = asset
	}
	for _, symbol := range symbols {
		if _, ok := bySymbol[symbol]; !ok {
			return nil, fmt.Errorf("required asset %q was not discovered", symbol)
		}
	}
	var capped []string
	if err := c.post(ctx, map[string]any{"type": "perpsAtOpenInterestCap", "dex": dexFor(symbols)}, &capped); err != nil {
		return nil, err
	}
	capSet := make(map[string]struct{}, len(capped))
	for _, symbol := range capped {
		capSet[symbol] = struct{}{}
	}
	result := make([]market.Snapshot, 0, len(symbols))
	for _, symbol := range symbols {
		var bookRaw json.RawMessage
		if err := c.post(ctx, map[string]any{"type": "l2Book", "coin": symbol}, &bookRaw); err != nil {
			return nil, err
		}
		book, err := ParseBook(bookRaw, symbol)
		if err != nil {
			return nil, err
		}
		asset := bySymbol[symbol]
		_, atCap := capSet[symbol]
		result = append(result, market.Snapshot{
			Symbol: symbol, MarkPrice: asset.MarkPrice, OraclePrice: asset.OraclePrice,
			OpenInterest: asset.OpenInterest, Funding: asset.Funding,
			ChangePercent: asset.ChangePercent,
			BestBid:       book.BestBid.Price, BestAsk: book.BestAsk.Price,
			ExchangeTime: time.UnixMilli(book.TimeMillis), ReceivedAt: receivedAt,
			Source: "hyperliquid", MarketStatus: "CONTINUOUS", Generation: generation, Delisted: asset.Delisted,
			AtOpenInterestCap: atCap,
		})
	}
	return result, nil
}

func (c *Client) post(ctx context.Context, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.infoURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Hyperliquid request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "stocks-marketing/1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Hyperliquid info: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Hyperliquid info returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("decode Hyperliquid response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("Hyperliquid response contains trailing data")
	}
	return nil
}

func dexFor(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	for i, ch := range symbols[0] {
		if ch == ':' {
			return symbols[0][:i]
		}
	}
	return ""
}
