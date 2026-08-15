package dart

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

const (
	supportedCreator = "SK하이닉스"
	supportedReport  = "파생상품거래손실발생"
	titlePrefix      = "(유가)SK하이닉스 - "
)

var receiptPattern = regexp.MustCompile(`^[0-9]{14}$`)

type Feed struct {
	ResponseHash string
	Disclosures  []Disclosure
}

type Disclosure struct {
	ReceiptNumber string
	ReportName    string
	Link          string
	Creator       string
	PublishedAt   time.Time
	Revision      bool
}

type Selection struct {
	Availability string
	Reason       string
	Event        domain.CatalystEvent
}

func ParseFeed(raw []byte) (Feed, error) {
	var payload struct {
		ChannelTitle string `xml:"channel>title"`
		ChannelLink  string `xml:"channel>link"`
		Items        []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			GUID    string `xml:"guid"`
			PubDate string `xml:"pubDate"`
			Creator string `xml:"http://purl.org/dc/elements/1.1/ creator"`
			Date    string `xml:"http://purl.org/dc/elements/1.1/ date"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(raw, &payload); err != nil {
		return Feed{}, fmt.Errorf("decode DART RSS: %w", err)
	}
	if strings.TrimSpace(payload.ChannelTitle) != "DART : (유가)SK하이닉스의 공시" || strings.TrimSpace(payload.ChannelLink) != "https://dart.fss.or.kr" {
		return Feed{}, errors.New("DART RSS has unexpected company identity")
	}
	if len(payload.Items) == 0 {
		return Feed{}, errors.New("DART RSS contains no disclosures")
	}
	digest := sha256.Sum256(raw)
	feed := Feed{ResponseHash: hex.EncodeToString(digest[:]), Disclosures: make([]Disclosure, 0, len(payload.Items))}
	for _, item := range payload.Items {
		publishedAt, err := parseRSSDate(item.PubDate)
		if err != nil {
			return Feed{}, errors.New("DART disclosure has invalid pubDate")
		}
		dcDate, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.Date))
		if err != nil || !publishedAt.Equal(dcDate) {
			return Feed{}, errors.New("DART disclosure timestamps conflict")
		}
		receiptNumber, canonicalLink, err := parseDisclosureLink(item.Link)
		if err != nil {
			return Feed{}, err
		}
		guidReceipt, canonicalGUID, err := parseDisclosureLink(item.GUID)
		if err != nil || guidReceipt != receiptNumber || canonicalGUID != canonicalLink {
			return Feed{}, errors.New("DART disclosure guid conflicts with link")
		}
		reportName, revision, err := parseReportName(strings.TrimSpace(item.Title))
		if err != nil {
			return Feed{}, err
		}
		creator := strings.TrimSpace(item.Creator)
		if creator == "" {
			return Feed{}, errors.New("DART disclosure creator is empty")
		}
		feed.Disclosures = append(feed.Disclosures, Disclosure{
			ReceiptNumber: receiptNumber, ReportName: reportName, Link: canonicalLink,
			Creator: creator, PublishedAt: publishedAt.UTC(), Revision: revision,
		})
	}
	return feed, nil
}

func SelectCatalyst(feed Feed, asOf time.Time, targetSymbol string) Selection {
	asOf = asOf.UTC()
	candidates := make([]Disclosure, 0)
	for _, disclosure := range feed.Disclosures {
		if disclosure.Creator == supportedCreator && disclosure.ReportName == supportedReport && !disclosure.PublishedAt.After(asOf) {
			candidates = append(candidates, disclosure)
		}
	}
	if len(candidates) == 0 {
		return Selection{Availability: domain.AvailabilityUnavailable, Reason: domain.CatalystReasonNoRecentSupportedEvent}
	}
	slices.SortFunc(candidates, func(left, right Disclosure) int {
		if order := left.PublishedAt.Compare(right.PublishedAt); order != 0 {
			return -order
		}
		return strings.Compare(right.ReceiptNumber, left.ReceiptNumber)
	})
	selected := candidates[0]
	if selected.Revision {
		return Selection{Availability: domain.AvailabilityDataConflict, Reason: domain.CatalystReasonRevisionUnsupported}
	}
	timestamp := selected.PublishedAt.Format(time.RFC3339Nano)
	return Selection{
		Availability: domain.AvailabilityAvailable,
		Event: domain.CatalystEvent{
			SourceEventID: selected.ReceiptNumber, PublishedAt: timestamp, EventAt: timestamp,
			Source: "dart", SourceTier: "OFFICIAL", OriginalSource: selected.Link,
			Category: domain.CatalystCategoryDerivativeTradingLoss, AffectedAssets: []string{targetSymbol},
			Importance: "HIGH", FactStatus: "CONFIRMED", ExpectedDirection: domain.DirectionBearish,
			EvidenceRefs: []string{"dart:rcpNo:" + selected.ReceiptNumber, "dart:rss:sha256:" + feed.ResponseHash},
		},
	}
}

func parseRSSDate(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("invalid RSS date")
}

func parseDisclosureLink(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host != "dart.fss.or.kr" || parsed.Path != "/api/link.jsp" || parsed.User != nil || parsed.Fragment != "" {
		return "", "", errors.New("DART disclosure link is not trusted")
	}
	query := parsed.Query()
	receipt := query.Get("rcpNo")
	if !receiptPattern.MatchString(receipt) || len(query) != 1 || len(query["rcpNo"]) != 1 {
		return "", "", errors.New("DART disclosure link has invalid rcpNo")
	}
	parsed.RawQuery = url.Values{"rcpNo": []string{receipt}}.Encode()
	return receipt, parsed.String(), nil
}

func parseReportName(title string) (string, bool, error) {
	if !strings.HasPrefix(title, titlePrefix) {
		return "", false, errors.New("DART disclosure title has unexpected company prefix")
	}
	report := strings.TrimSpace(strings.TrimPrefix(title, titlePrefix))
	revision := false
	for {
		matched := false
		for _, prefix := range []string{"[기재정정]", "[첨부정정]", "[첨부추가]", "[정정]", "[철회]"} {
			if strings.HasPrefix(report, prefix) {
				report = strings.TrimSpace(strings.TrimPrefix(report, prefix))
				revision = true
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	if report == "" {
		return "", false, errors.New("DART disclosure report name is empty")
	}
	return report, revision, nil
}
