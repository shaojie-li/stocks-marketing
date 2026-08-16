package opendart

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"io"
	"math/big"
	"regexp"
	"strings"
	"time"
)

type periodicReport struct{ ReceiptNo, ReportCode, BusinessYear, PeriodEnd, PublishedAt string }
type documentFacts struct{ AIDemandSalesDirection, AIDemandShipmentDirection, DRAMASPDirection, NANDASPDirection string }
type financialProjection struct {
	Revenue, PriorRevenue, OperatingProfit, PriorOperatingProfit, Inventory, Cash, Borrowings, CapEx, OperatingCashFlow string
	PriorCash, PriorBorrowings                                                                                          string
}
type apiResponse struct {
	Status, Message string
	List            []map[string]string `json:"list"`
}

var reportPattern = regexp.MustCompile(`^(분기보고서|반기보고서|사업보고서) \(([0-9]{4})\.([0-9]{2})\)$`)
var tagsPattern = regexp.MustCompile(`(?s)<[^>]*>`)

func parseLatestReport(raw []byte, asOf string) (periodicReport, error) {
	var payload apiResponse
	if json.Unmarshal(raw, &payload) != nil || payload.Status != "000" || len(payload.List) == 0 {
		return periodicReport{}, errors.New("invalid OpenDART report list")
	}
	cutoff, err := time.Parse(time.RFC3339Nano, asOf)
	if err != nil {
		return periodicReport{}, errors.New("invalid report as_of")
	}
	var selected periodicReport
	var selectedPublished time.Time
	for _, item := range payload.List {
		match := reportPattern.FindStringSubmatch(item["report_nm"])
		if match == nil || item["corp_code"] != "00164779" || !regexp.MustCompile(`^[0-9]{14}$`).MatchString(item["rcept_no"]) {
			continue
		}
		published, parseErr := time.Parse("20060102", item["rcept_dt"])
		if parseErr != nil || published.After(cutoff) {
			continue
		}
		code := map[string]string{"분기보고서": "11013", "반기보고서": "11012", "사업보고서": "11011"}[match[1]]
		if match[1] == "분기보고서" && match[3] != "03" {
			code = "11014"
		}
		year := match[2]
		month := match[3]
		m, _ := time.Parse("2006-01", year+"-"+month)
		end := m.AddDate(0, 1, 0).AddDate(0, 0, -1)
		if selected.ReceiptNo == "" || published.After(selectedPublished) || published.Equal(selectedPublished) && item["rcept_no"] > selected.ReceiptNo {
			selected = periodicReport{ReceiptNo: item["rcept_no"], ReportCode: code, BusinessYear: year, PeriodEnd: end.Format("2006-01-02"), PublishedAt: published.UTC().Format(time.RFC3339Nano)}
			selectedPublished = published
		}
	}
	if selected.ReceiptNo != "" {
		return selected, nil
	}
	return periodicReport{}, errors.New("no supported final OpenDART report")
}

func parseFinancials(raw []byte, receipt, reportCode string) (financialProjection, error) {
	var payload apiResponse
	if json.Unmarshal(raw, &payload) != nil || payload.Status != "000" {
		return financialProjection{}, errors.New("invalid OpenDART financials")
	}
	accounts := map[string]map[string]string{}
	wanted := map[string]string{"ifrs-full_Revenue": "CIS", "dart_OperatingIncomeLoss": "CIS", "ifrs-full_Inventories": "BS", "ifrs-full_CashAndCashEquivalents": "BS", "ifrs-full_CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings": "BS", "ifrs-full_LongtermBorrowings": "BS", "ifrs-full_CashFlowsFromUsedInOperatingActivities": "CF", "ifrs-full_PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities": "CF"}
	for _, item := range payload.List {
		id := item["account_id"]
		statement, ok := wanted[id]
		if !ok || item["sj_div"] != statement {
			continue
		}
		if _, exists := accounts[id]; exists {
			return financialProjection{}, errors.New("duplicate OpenDART core account")
		}
		if item["rcept_no"] != receipt || item["reprt_code"] != reportCode || item["corp_code"] != "00164779" || item["currency"] != "KRW" {
			return financialProjection{}, errors.New("conflicting OpenDART core account")
		}
		accounts[id] = item
	}
	for id := range wanted {
		if accounts[id] == nil {
			return financialProjection{}, errors.New("missing OpenDART core account")
		}
	}
	normalize := func(value string) (string, error) {
		v, ok := new(big.Int).SetString(strings.ReplaceAll(value, ",", ""), 10)
		if !ok || v.Sign() < 0 {
			return "", errors.New("invalid OpenDART amount")
		}
		return v.String(), nil
	}
	value := func(id, field string) (string, error) { return normalize(accounts[id][field]) }
	revenue, e := value("ifrs-full_Revenue", "thstrm_add_amount")
	if e != nil {
		return financialProjection{}, e
	}
	priorRevenue, e := value("ifrs-full_Revenue", "frmtrm_add_amount")
	if e != nil {
		return financialProjection{}, e
	}
	op, e := value("dart_OperatingIncomeLoss", "thstrm_add_amount")
	if e != nil {
		return financialProjection{}, e
	}
	priorOP, e := value("dart_OperatingIncomeLoss", "frmtrm_add_amount")
	if e != nil {
		return financialProjection{}, e
	}
	inv, e := value("ifrs-full_Inventories", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	cash, e := value("ifrs-full_CashAndCashEquivalents", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	short, e := value("ifrs-full_CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	long, e := value("ifrs-full_LongtermBorrowings", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	debt := new(big.Int)
	debt.Add(mustInt(short), mustInt(long))
	priorCash, e := value("ifrs-full_CashAndCashEquivalents", "frmtrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	priorShort, e := value("ifrs-full_CurrentBorrowingsAndCurrentPortionOfNoncurrentBorrowings", "frmtrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	priorLong, e := value("ifrs-full_LongtermBorrowings", "frmtrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	priorDebt := new(big.Int).Add(mustInt(priorShort), mustInt(priorLong))
	capex, e := value("ifrs-full_PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	ocf, e := value("ifrs-full_CashFlowsFromUsedInOperatingActivities", "thstrm_amount")
	if e != nil {
		return financialProjection{}, e
	}
	return financialProjection{
		Revenue: revenue, PriorRevenue: priorRevenue,
		OperatingProfit: op, PriorOperatingProfit: priorOP,
		Inventory: inv, Cash: cash, Borrowings: debt.String(),
		CapEx: capex, OperatingCashFlow: ocf,
		PriorCash: priorCash, PriorBorrowings: priorDebt.String(),
	}, nil
}

func mustInt(value string) *big.Int { result, _ := new(big.Int).SetString(value, 10); return result }

func parseDocument(raw []byte, receipt string) (documentFacts, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != receipt+".xml" {
		return documentFacts{}, errors.New("invalid OpenDART document archive")
	}
	reader, err := archive.File[0].Open()
	if err != nil {
		return documentFacts{}, errors.New("open OpenDART document")
	}
	defer reader.Close()
	document, err := io.ReadAll(io.LimitReader(reader, 16<<20))
	if err != nil || len(document) == 16<<20 {
		return documentFacts{}, errors.New("read OpenDART document")
	}
	text := strings.Join(strings.Fields(html.UnescapeString(string(tagsPattern.ReplaceAll(document, []byte(" "))))), " ")
	start := strings.Index(text, "나. 주요 제품 등의 가격변동추이")
	if start < 0 {
		return documentFacts{}, errors.New("missing OpenDART price section")
	}
	text = text[start:]
	end := strings.Index(text, "3. 원재료 및 생산설비")
	if end < 0 {
		return documentFacts{}, errors.New("unterminated OpenDART price section")
	}
	section := text[:end]
	nand := strings.Index(section, "NAND")
	if nand < 0 {
		return documentFacts{}, errors.New("missing OpenDART NAND section")
	}
	dramText, nandText := section[:nand], section[nand:]
	direction := func(value string) string {
		if strings.Contains(value, "ASP") && strings.Contains(value, "상승") {
			return "INCREASE"
		}
		if strings.Contains(value, "ASP") && strings.Contains(value, "하락") {
			return "DECREASE"
		}
		return ""
	}
	facts := documentFacts{DRAMASPDirection: direction(dramText), NANDASPDirection: direction(nandText)}
	if strings.Contains(dramText, "HBM") && strings.Contains(dramText, "AI향 서버 DRAM") && strings.Contains(dramText, "판매를 확대") {
		facts.AIDemandSalesDirection = "INCREASE"
	}
	if strings.Contains(dramText, "출하량") && strings.Contains(dramText, "증가") {
		facts.AIDemandShipmentDirection = "INCREASE"
	}
	if facts.DRAMASPDirection == "" || facts.NANDASPDirection == "" {
		return documentFacts{}, errors.New("missing OpenDART ASP direction")
	}
	return facts, nil
}
