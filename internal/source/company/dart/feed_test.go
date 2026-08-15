package dart

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

func TestParseFeedSelectsOnlySupportedCompanyDisclosure(t *testing.T) {
	raw := rssFixture(t)
	feed, err := ParseFeed(raw)
	if err != nil {
		t.Fatalf("parse DART feed: %v", err)
	}
	if len(feed.Disclosures) != 3 {
		t.Fatalf("parsed disclosures = %d, want supported, unrelated report, and exchange-originated items", len(feed.Disclosures))
	}
	selection := SelectCatalyst(feed, time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC), "xyz:SKHY")
	if selection.Availability != domain.AvailabilityAvailable || selection.Reason != "" {
		t.Fatalf("selection = %#v", selection)
	}
	event := selection.Event
	if event.SourceEventID != "20260814802986" || event.SourceTier != "OFFICIAL" || event.Category != domain.CatalystCategoryDerivativeTradingLoss || event.ExpectedDirection != domain.DirectionBearish {
		t.Fatalf("event identity or direction = %#v", event)
	}
	if event.EventAt != "2026-08-14T07:44:00Z" || event.PublishedAt != event.EventAt || event.OriginalSource != "https://dart.fss.or.kr/api/link.jsp?rcpNo=20260814802986" {
		t.Fatalf("event audit fields = %#v", event)
	}
	if len(event.AffectedAssets) != 1 || event.AffectedAssets[0] != "xyz:SKHY" || len(event.EvidenceRefs) != 2 {
		t.Fatalf("event evidence = %#v", event)
	}
}

func TestParseFeedRejectsConflictingTimeAndUntrustedLink(t *testing.T) {
	raw := rssFixture(t)
	for name, payload := range map[string][]byte{
		"conflicting time": bytes.Replace(raw, []byte("2026-08-14T07:44:00Z"), []byte("2026-08-14T07:45:00Z"), 1),
		"untrusted link":   bytes.Replace(raw, []byte("https://dart.fss.or.kr/api/link.jsp?rcpNo=20260814802986"), []byte("https://example.com/api/link.jsp?rcpNo=20260814802986"), -1),
		"wrong company":    bytes.Replace(raw, []byte("DART : (유가)SK하이닉스의 공시"), []byte("DART : (유가)다른회사의 공시"), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFeed(payload); err == nil {
				t.Fatal("invalid DART feed was accepted")
			}
		})
	}
}

func TestSelectCatalystFailsClosedForRevisionAndNoRecentEvent(t *testing.T) {
	raw := rssFixture(t)
	revised, err := ParseFeed(bytes.Replace(raw, []byte("파생상품거래손실발생"), []byte("[기재정정]파생상품거래손실발생"), 1))
	if err != nil {
		t.Fatal(err)
	}
	selection := SelectCatalyst(revised, time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC), "xyz:SKHY")
	if selection.Availability != domain.AvailabilityDataConflict || selection.Reason != domain.CatalystReasonRevisionUnsupported {
		t.Fatalf("revision selection = %#v", selection)
	}

	feed, err := ParseFeed(raw)
	if err != nil {
		t.Fatal(err)
	}
	selection = SelectCatalyst(feed, time.Date(2026, 8, 14, 7, 43, 59, 0, time.UTC), "xyz:SKHY")
	if selection.Availability != domain.AvailabilityUnavailable || selection.Reason != domain.CatalystReasonNoRecentSupportedEvent {
		t.Fatalf("future event selection = %#v", selection)
	}
}

func rssFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "data-sources", "dart", "t010-skhy-company-rss.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
