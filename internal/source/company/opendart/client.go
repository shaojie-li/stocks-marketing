package opendart

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

type Client struct {
	httpClient *http.Client
	config     Config
	retryDelay func(int) time.Duration
}

func NewClient(httpClient *http.Client, config Config) *Client {
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ForceAttemptHTTP2 = false
		transport.TLSHandshakeTimeout = config.RequestTimeout
		// OpenDART currently negotiates this TLS 1.2 RSA suite, which modern Go no longer enables by default.
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}}
		httpClient = &http.Client{Timeout: config.RequestTimeout, Transport: transport}
	}
	return &Client{httpClient: httpClient, config: config, retryDelay: func(attempt int) time.Duration {
		return time.Duration(attempt+1) * 250 * time.Millisecond
	}}
}

func (c *Client) LatestFundamental(ctx context.Context, asOf time.Time) (domain.Fundamental, error) {
	asOf = asOf.UTC()
	start := time.Date(asOf.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
	list, listHash, err := c.get(ctx, "list.json", url.Values{"corp_code": {c.config.CorpCode}, "bgn_de": {start.Format("20060102")}, "end_de": {asOf.Format("20060102")}, "pblntf_ty": {"A"}, "last_reprt_at": {"Y"}, "page_count": {"100"}}, 8<<20)
	if err != nil {
		return domain.Fundamental{}, err
	}
	report, err := parseLatestReport(list, asOf.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Fundamental{}, err
	}
	published, _ := time.Parse(time.RFC3339Nano, report.PublishedAt)
	if asOf.Sub(published) > 150*24*time.Hour {
		return domain.UnavailableFundamental(c.config.TargetSymbol, "STALE"), nil
	}
	current, currentHash, err := c.financials(ctx, report.BusinessYear, report.ReportCode)
	if err != nil {
		return domain.Fundamental{}, err
	}
	year, _ := strconv.Atoi(report.BusinessYear)
	priorRaw, priorHash, err := c.financials(ctx, strconv.Itoa(year-1), report.ReportCode)
	if err != nil {
		return domain.Fundamental{}, err
	}
	currentFacts, err := parseFinancials(current, report.ReceiptNo, report.ReportCode)
	if err != nil {
		return domain.Fundamental{}, err
	}
	var priorPayload apiResponse
	if json.Unmarshal(priorRaw, &priorPayload) != nil || len(priorPayload.List) == 0 {
		return domain.Fundamental{}, errors.New("invalid prior OpenDART financials")
	}
	priorReceipt := priorPayload.List[0]["rcept_no"]
	priorFacts, err := parseFinancials(priorRaw, priorReceipt, report.ReportCode)
	if err != nil {
		return domain.Fundamental{}, err
	}
	document, documentHash, err := c.get(ctx, "document.xml", url.Values{"rcept_no": {report.ReceiptNo}}, 64<<20)
	if err != nil {
		return domain.Fundamental{}, err
	}
	narrative, err := parseDocument(document, report.ReceiptNo)
	if err != nil {
		return domain.Fundamental{}, err
	}
	refs := []string{"opendart:list:sha256:" + listHash, "opendart:financials:sha256:" + currentHash, "opendart:prior-financials:sha256:" + priorHash, "opendart:document:sha256:" + documentHash, "opendart:rcept_no:" + report.ReceiptNo}
	return domain.CalculateFundamental(domain.FundamentalInput{Symbol: c.config.TargetSymbol, AsOf: asOf.Format(time.RFC3339Nano), ReceiptNo: report.ReceiptNo, PublishedAt: report.PublishedAt, PeriodEnd: report.PeriodEnd, EvidenceRefs: refs, AIDemandSalesDirection: narrative.AIDemandSalesDirection, AIDemandShipmentDirection: narrative.AIDemandShipmentDirection, DRAMASPDirection: narrative.DRAMASPDirection, NANDASPDirection: narrative.NANDASPDirection, Revenue: currentFacts.Revenue, PriorRevenue: currentFacts.PriorRevenue, OperatingProfit: currentFacts.OperatingProfit, PriorOperatingProfit: currentFacts.PriorOperatingProfit, Inventory: currentFacts.Inventory, PriorInventory: priorFacts.Inventory, CapEx: currentFacts.CapEx, PriorCapEx: priorFacts.CapEx, OperatingCashFlow: currentFacts.OperatingCashFlow, PriorOperatingCashFlow: priorFacts.OperatingCashFlow, Cash: currentFacts.Cash, PriorCash: currentFacts.PriorCash, Borrowings: currentFacts.Borrowings, PriorBorrowings: currentFacts.PriorBorrowings}), nil
}

func (c *Client) financials(ctx context.Context, year, code string) ([]byte, string, error) {
	return c.get(ctx, "fnlttSinglAcntAll.json", url.Values{"corp_code": {c.config.CorpCode}, "bsns_year": {year}, "reprt_code": {code}, "fs_div": {"CFS"}}, 8<<20)
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, limit int64) ([]byte, string, error) {
	query.Set("crtfc_key", c.config.APIKey)
	requestURL := c.config.BaseURL + "/" + endpoint + "?" + query.Encode()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, "", errors.New("create OpenDART request")
		}
		request.Header.Set("Accept", "application/json, application/zip;q=0.9")
		request.Header.Set("User-Agent", "stocks-marketing/1")
		response, err := c.httpClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			last = errors.New("call OpenDART")
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				return nil, "", errors.New("read OpenDART response")
			}
			if int64(len(raw)) > limit {
				return nil, "", errors.New("OpenDART response exceeds limit")
			}
			if response.StatusCode == http.StatusOK {
				digest := sha256.Sum256(raw)
				return raw, hex.EncodeToString(digest[:]), nil
			}
			last = fmt.Errorf("OpenDART returned HTTP %d", response.StatusCode)
			if response.StatusCode != 408 && response.StatusCode != 429 && response.StatusCode < 500 {
				return nil, "", last
			}
		}
		if attempt < 2 {
			timer := time.NewTimer(c.retryDelay(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, "", errors.New("OpenDART retry canceled")
			case <-timer.C:
			}
		}
	}
	return nil, "", last
}
