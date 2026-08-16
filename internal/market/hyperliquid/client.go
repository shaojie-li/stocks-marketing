package hyperliquid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
	"github.com/shaojie-li/stocks-marketing/internal/market"
)

const (
	maxResponseBytes           = 4 << 20
	hyperliquidRequestAttempts = 3
)

type Client struct {
	httpClient *http.Client
	infoURL    string
	timeout    time.Duration
	now        func() time.Time
	retryDelay func(int) time.Duration
}

func NewClient(httpClient *http.Client, infoURL string, timeout time.Duration) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient: httpClient, infoURL: infoURL, timeout: timeout, now: time.Now,
		retryDelay: hyperliquidFullJitterDelay,
	}
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

func (c *Client) CatalystPriceWindow(ctx context.Context, eventAt, asOf time.Time, targetSymbol, benchmarkSymbol string) (domain.CatalystPriceWindow, error) {
	if targetSymbol != "xyz:SKHY" || benchmarkSymbol != "xyz:SMSN" {
		return domain.CatalystPriceWindow{}, errors.New("Catalyst requires xyz:SKHY and xyz:SMSN")
	}
	bounds, err := CatalystWindowBounds(eventAt, asOf)
	if err != nil {
		return domain.CatalystPriceWindow{}, err
	}
	type prices struct {
		start string
		end   string
		ref   string
	}
	bySymbol := make(map[string]prices, 2)
	for _, symbol := range []string{targetSymbol, benchmarkSymbol} {
		var raw json.RawMessage
		payload := map[string]any{
			"type": "candleSnapshot",
			"req": map[string]any{
				"coin": symbol, "interval": "1m",
				"startTime": bounds.RequestStart.UnixMilli(), "endTime": bounds.WindowEnd.UnixMilli(),
			},
		}
		if err := c.post(ctx, payload, &raw); err != nil {
			return domain.CatalystPriceWindow{}, fmt.Errorf("load CATALYST_24H candles for %s: %w", symbol, err)
		}
		candles, err := ParseCandles(raw, symbol, "1m")
		if err != nil {
			return domain.CatalystPriceWindow{}, fmt.Errorf("load CATALYST_24H candles for %s: %w", symbol, err)
		}
		if !candles[0].CloseTime.Equal(bounds.WindowStart) || !candles[len(candles)-1].CloseTime.Equal(bounds.WindowEnd) {
			return domain.CatalystPriceWindow{}, fmt.Errorf("SKIPPED_SOURCE_INCOMPLETE: %s Catalyst candle anchors do not match", symbol)
		}
		digest := sha256.Sum256(raw)
		bySymbol[symbol] = prices{
			start: candles[0].Close, end: candles[len(candles)-1].Close,
			ref: fmt.Sprintf("hyperliquid:candleSnapshot:%s:1m:sha256:%x", symbol, digest),
		}
	}
	target, benchmark := bySymbol[targetSymbol], bySymbol[benchmarkSymbol]
	return domain.CatalystPriceWindow{
		TargetSymbol: targetSymbol, BenchmarkSymbol: benchmarkSymbol,
		WindowStart: bounds.WindowStart.Format(time.RFC3339Nano), WindowEnd: bounds.WindowEnd.Format(time.RFC3339Nano),
		TargetStartPrice: target.start, TargetEndPrice: target.end,
		BenchmarkStartPrice: benchmark.start, BenchmarkEndPrice: benchmark.end,
		EvidenceRefs: []string{target.ref, benchmark.ref},
	}, nil
}

func (c *Client) SKHYPriceStructure(ctx context.Context, symbol string, asOf time.Time) (domain.PriceStructure, error) {
	if symbol != "xyz:SKHY" {
		return domain.PriceStructure{}, errors.New("SKHY Price Structure requires xyz:SKHY")
	}
	asOf = asOf.UTC()
	var raw json.RawMessage
	payload := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin": symbol, "interval": "1d", "startTime": int64(0), "endTime": asOf.UnixMilli(),
		},
	}
	if err := c.post(ctx, payload, &raw); err != nil {
		return domain.PriceStructure{}, fmt.Errorf("load SKHY daily candles: %w", err)
	}
	candles, err := ParseCandles(raw, symbol, "1d")
	if err != nil {
		return domain.PriceStructure{}, fmt.Errorf("load SKHY daily candles: %w", err)
	}
	bars := make([]domain.DailyPriceBar, len(candles))
	for index, candle := range candles {
		bars[index] = domain.DailyPriceBar{
			Symbol: candle.Symbol, Interval: candle.Interval,
			OpenTime: candle.OpenTime.Format(time.RFC3339Nano), CloseTime: candle.CloseTime.Format(time.RFC3339Nano),
			Open: candle.Open, Close: candle.Close, High: candle.High, Low: candle.Low, Volume: candle.Volume,
		}
	}
	digest := sha256.Sum256(raw)
	evidenceRef := fmt.Sprintf("hyperliquid:candleSnapshot:%s:1d:sha256:%x", symbol, digest)
	return domain.CalculatePriceStructure(domain.PriceStructureInput{
		Symbol: symbol, AsOf: asOf.Format(time.RFC3339Nano), Bars: bars, EvidenceRefs: []string{evidenceRef},
	}), nil
}

func (c *Client) SKHYCrowding(ctx context.Context, symbol string, asOf time.Time) (domain.Crowding, error) {
	if symbol != "xyz:SKHY" {
		return domain.Crowding{}, errors.New("Crowding requires xyz:SKHY")
	}
	asOf = asOf.UTC()
	if asOf.IsZero() {
		return domain.Crowding{}, errors.New("Crowding requires as_of")
	}
	input := domain.CrowdingInput{Symbol: symbol, AsOf: asOf.Format(time.RFC3339Nano)}

	var contextRaw json.RawMessage
	if err := c.post(ctx, map[string]any{"type": "metaAndAssetCtxs", "dex": dexFor([]string{symbol})}, &contextRaw); err == nil {
		if contexts, parseErr := ParseMetaAndAssetContexts(contextRaw); parseErr == nil {
			for _, asset := range contexts {
				if asset.Symbol == symbol {
					input.CurrentOpenInterest = asset.OpenInterest
					digest := sha256.Sum256(contextRaw)
					input.OIEvidenceRefs = []string{fmt.Sprintf("hyperliquid:metaAndAssetCtxs:%s:sha256:%x", symbol, digest)}
					break
				}
			}
		}
	}

	input.FundingHistory, input.FundingEvidenceRefs, input.FundingReason = c.crowdingFundingHistory(ctx, symbol, asOf)
	input.DailyBars, input.DailyEvidenceRefs, input.DailyReason = c.crowdingDailyBars(ctx, symbol, asOf)
	return domain.CalculateCrowding(input), nil
}

func (c *Client) crowdingFundingHistory(ctx context.Context, symbol string, asOf time.Time) ([]domain.CrowdingFundingSample, []string, string) {
	const maxFundingPages = 3
	latestExpected := asOf.Truncate(time.Hour)
	if !latestExpected.Before(asOf) {
		latestExpected = latestExpected.Add(-time.Hour)
	}
	cursor := latestExpected.Add(-719 * time.Hour).UnixMilli()
	history := make([]domain.CrowdingFundingSample, 0, 720)
	refs := make([]string, 0, 2)
	for page := 0; page < maxFundingPages; page++ {
		var raw json.RawMessage
		payload := map[string]any{"type": "fundingHistory", "coin": symbol, "startTime": cursor, "endTime": asOf.UnixMilli()}
		if err := c.post(ctx, payload, &raw); err != nil {
			return nil, refs, domain.CrowdingReasonSourceError
		}
		samples, err := ParseFundingHistory(raw, symbol)
		if err != nil {
			return nil, refs, domain.CrowdingReasonInvalidHistory
		}
		digest := sha256.Sum256(raw)
		refs = append(refs, fmt.Sprintf("hyperliquid:fundingHistory:%s:sha256:%x", symbol, digest))
		history = append(history, samples...)
		if len(samples) < 500 {
			return history, refs, ""
		}
		last, err := time.Parse(time.RFC3339Nano, samples[len(samples)-1].Time)
		if err != nil || last.UnixMilli()+1 <= cursor {
			return nil, refs, domain.CrowdingReasonInvalidHistory
		}
		cursor = last.UnixMilli() + 1
	}
	return nil, refs, domain.CrowdingReasonInvalidHistory
}

func (c *Client) crowdingDailyBars(ctx context.Context, symbol string, asOf time.Time) ([]domain.DailyPriceBar, []string, string) {
	var raw json.RawMessage
	payload := map[string]any{
		"type": "candleSnapshot",
		"req":  map[string]any{"coin": symbol, "interval": "1d", "startTime": int64(0), "endTime": asOf.UnixMilli()},
	}
	if err := c.post(ctx, payload, &raw); err != nil {
		return nil, nil, domain.CrowdingReasonSourceError
	}
	candles, err := ParseCandles(raw, symbol, "1d")
	if err != nil {
		return nil, nil, domain.CrowdingReasonInvalidHistory
	}
	bars := make([]domain.DailyPriceBar, len(candles))
	for index, candle := range candles {
		bars[index] = domain.DailyPriceBar{
			Symbol: candle.Symbol, Interval: candle.Interval,
			OpenTime: candle.OpenTime.Format(time.RFC3339Nano), CloseTime: candle.CloseTime.Format(time.RFC3339Nano),
			Open: candle.Open, Close: candle.Close, High: candle.High, Low: candle.Low, Volume: candle.Volume,
		}
	}
	digest := sha256.Sum256(raw)
	return bars, []string{fmt.Sprintf("hyperliquid:candleSnapshot:%s:1d:sha256:%x", symbol, digest)}, ""
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
	var lastErr error
	for attempt := 0; attempt < hyperliquidRequestAttempts; attempt++ {
		retryable, err := c.postOnce(ctx, body, result)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == hyperliquidRequestAttempts-1 {
			return err
		}
		timer := time.NewTimer(c.retryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func (c *Client) postOnce(ctx context.Context, body []byte, result any) (bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.infoURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("create Hyperliquid request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "stocks-marketing/1")
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return true, fmt.Errorf("call Hyperliquid info: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		retryable := retryableHTTPStatus(response.StatusCode)
		return retryable, fmt.Errorf("Hyperliquid info returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err := decoder.Decode(result); err != nil {
		return false, fmt.Errorf("decode Hyperliquid response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false, errors.New("Hyperliquid response contains trailing data")
	}
	return false, nil
}

func hyperliquidFullJitterDelay(attempt int) time.Duration {
	maximum := time.Second * time.Duration(1<<attempt)
	if maximum > 4*time.Second {
		maximum = 4 * time.Second
	}
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
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
