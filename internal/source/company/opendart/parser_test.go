package opendart

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseLatestReportSelectsSupportedFinalPeriodicReport(t *testing.T) {
	report, err := parseLatestReport([]byte(`{"status":"000","message":"정상","list":[{"corp_code":"00164779","rcept_no":"20260515000123","report_nm":"분기보고서 (2026.03)","rcept_dt":"20260515"},{"corp_code":"00164779","rcept_no":"20260814003509","report_nm":"반기보고서 (2026.06)","rcept_dt":"20260814"}]}`), "2026-08-16T00:00:00Z")
	if err != nil || report.ReceiptNo != "20260814003509" || report.ReportCode != "11012" || report.BusinessYear != "2026" || report.PeriodEnd != "2026-06-30" {
		t.Fatalf("report = %#v, err = %v", report, err)
	}
}

func TestParseFinancialsRejectsDuplicateCoreAccount(t *testing.T) {
	raw := []byte(`{"status":"000","message":"정상","list":[{"rcept_no":"20260814003509","corp_code":"00164779","reprt_code":"11012","sj_div":"CIS","account_id":"ifrs-full_Revenue","currency":"KRW","thstrm_add_amount":"100","frmtrm_add_amount":"80"},{"rcept_no":"20260814003509","corp_code":"00164779","reprt_code":"11012","sj_div":"CIS","account_id":"ifrs-full_Revenue","currency":"KRW","thstrm_add_amount":"100","frmtrm_add_amount":"80"}]}`)
	if _, err := parseFinancials(raw, "20260814003509", "11012"); err == nil {
		t.Fatal("duplicate core account was accepted")
	}
}

func TestParseDocumentUsesOnlyPriceChangeSection(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, _ := archive.Create("20260814003509.xml")
	_, _ = file.Write([]byte(`<DOCUMENT><P>历史价格下降不应参与。</P><P>나. 주요 제품 등의 가격변동추이</P><P>HBM3E와 AI향 서버 DRAM 제품 중심으로 판매를 확대하여 출하량이 증가하였으며, ASP는 상승하였습니다. NAND 출하량이 증가하였으며, ASP는 상승하였습니다.</P><P>3. 원재료 및 생산설비</P></DOCUMENT>`))
	_ = archive.Close()
	facts, err := parseDocument(buffer.Bytes(), "20260814003509")
	if err != nil || facts.AIDemandSalesDirection != "INCREASE" || facts.AIDemandShipmentDirection != "INCREASE" || facts.DRAMASPDirection != "INCREASE" || facts.NANDASPDirection != "INCREASE" {
		t.Fatalf("facts = %#v, err = %v", facts, err)
	}
}
