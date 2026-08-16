package hyperliquid

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMetaAndAssetContextsPreservesIndexAndDecimalPrecision(t *testing.T) {
	raw := readFixture(t, "t004-meta-and-asset-ctxs.json")
	assets, err := ParseMetaAndAssetContexts(raw)
	if err != nil {
		t.Fatalf("parse first-party fixture: %v", err)
	}
	if got, want := len(assets), 2; got != want {
		t.Fatalf("asset count = %d, want %d", got, want)
	}
	if assets[0].Symbol != "xyz:MU" || assets[0].MarkPrice != "973.36" || assets[0].OpenInterest != "142814.6440000001" || assets[0].ChangePercent != "-0.31134781" {
		t.Fatalf("MU was misaligned or rounded: %#v", assets[0])
	}
	if assets[1].Symbol != "xyz:SKHY" || assets[1].MarkPrice != "166.18" {
		t.Fatalf("SKHY was misaligned: %#v", assets[1])
	}
}

func TestParseMetaAndAssetContextsRejectsUnsafePayloads(t *testing.T) {
	valid := string(readFixture(t, "t004-meta-and-asset-ctxs.json"))
	tests := map[string]string{
		"array length mismatch":   strings.Replace(valid, "    {\"funding\":\"0.0000001021\",\"openInterest\":\"1399928.0799999998\",\"prevDayPx\":\"167.42\",\"dayNtlVlm\":\"84367987.3520999253\",\"oraclePx\":\"166.22\",\"markPx\":\"166.18\",\"midPx\":\"166.185\"}\n", "", 1),
		"duplicate symbol":        strings.Replace(valid, "xyz:SKHY", "xyz:MU", 1),
		"invalid decimal":         strings.Replace(valid, "\"973.36\"", "\"NaN\"", 1),
		"fraction is not decimal": strings.Replace(valid, "\"973.36\"", "\"1/2\"", 1),
		"missing mark":            strings.Replace(valid, "\"markPx\":\"973.36\",", "", 1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseMetaAndAssetContexts([]byte(payload)); err == nil {
				t.Fatal("unsafe payload was accepted")
			}
		})
	}
}

func TestParseBookRejectsWrongAssetAndOneSidedBook(t *testing.T) {
	valid := []byte(`{"coin":"xyz:SKHY","time":1786788513827,"levels":[[{"px":"166.18","sz":"7.82","n":3}],[{"px":"166.19","sz":"10.66","n":2}]]}`)
	book, err := ParseBook(valid, "xyz:SKHY")
	if err != nil {
		t.Fatalf("parse book: %v", err)
	}
	if book.BestBid.Price != "166.18" || book.BestAsk.Price != "166.19" {
		t.Fatalf("unexpected best prices: %#v", book)
	}
	if _, err := ParseBook(valid, "xyz:MU"); err == nil {
		t.Fatal("book for wrong asset was accepted")
	}
	oneSided := []byte(`{"coin":"xyz:SKHY","time":1786788513827,"levels":[[{"px":"166.18","sz":"7.82","n":3}],[]]}`)
	if _, err := ParseBook(oneSided, "xyz:SKHY"); err == nil {
		t.Fatal("one-sided book was accepted")
	}
}

func TestParseFundingHistoryPreservesOfficialDecimalsAndHourlyTimes(t *testing.T) {
	samples, err := ParseFundingHistory(readFixture(t, "t012-skhy-funding-history.json"), "xyz:SKHY")
	if err != nil {
		t.Fatalf("parse funding history: %v", err)
	}
	if len(samples) != 2 || samples[0].FundingRate != "-0.0000164624" || samples[0].Premium != "-0.0005633981" || samples[1].Time != "2026-07-16T01:00:00.134Z" {
		t.Fatalf("funding history was changed: %#v", samples)
	}
}

func TestParseFundingHistoryRejectsMissingWrongOrMalformedFields(t *testing.T) {
	valid := string(readFixture(t, "t012-skhy-funding-history.json"))
	tests := map[string]string{
		"wrong symbol":    strings.Replace(valid, "xyz:SKHY", "xyz:OTHER", 1),
		"missing premium": strings.Replace(valid, `"premium": "-0.0005633981",`, "", 1),
		"invalid decimal": strings.Replace(valid, `"0.00000625"`, `"NaN"`, 1),
		"duplicate hour":  strings.Replace(valid, "1784163600134", "1784160000134", 1),
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFundingHistory([]byte(payload), "xyz:SKHY"); err == nil {
				t.Fatal("unsafe funding history was accepted")
			}
		})
	}
}

func TestParseWebSocketReplayRequiresKnownAssets(t *testing.T) {
	scanner := bufio.NewScanner(bytes.NewReader(readFixture(t, "t004-websocket-replay.jsonl")))
	known := map[string]struct{}{"xyz:SKHY": {}}
	var acknowledgements, books, contexts int
	for scanner.Scan() {
		message, err := ParseWebSocketMessage(scanner.Bytes(), known)
		if err != nil {
			t.Fatalf("parse replay message: %v", err)
		}
		switch message.Kind {
		case MessageSubscriptionAcknowledged:
			acknowledgements++
		case MessageBook:
			books++
		case MessageAssetContext:
			contexts++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if acknowledgements != 2 || books != 1 || contexts != 1 {
		t.Fatalf("unexpected replay counts: ack=%d book=%d context=%d", acknowledgements, books, contexts)
	}
	unknown := []byte(`{"channel":"l2Book","data":{"coin":"xyz:OTHER","time":1786788513827,"levels":[[{"px":"1","sz":"1","n":1}],[{"px":"2","sz":"1","n":1}]]}}`)
	if _, err := ParseWebSocketMessage(unknown, known); err == nil {
		t.Fatal("update for unknown asset was accepted")
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "data-sources", "hyperliquid", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
