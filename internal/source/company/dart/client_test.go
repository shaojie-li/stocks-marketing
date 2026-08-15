package dart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaojie-li/stocks-marketing/internal/domain"
)

func TestClientLatestCatalystRetriesTransientFailureWithinBound(t *testing.T) {
	fixture, err := os.ReadFile("../../../../testdata/data-sources/dart/t010-skhy-company-rss.xml")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/companyRSS.xml" || request.URL.Query().Get("crpCd") != "00164779" {
			t.Fatalf("unexpected DART request: %s", request.URL.String())
		}
		if request.Header.Get("User-Agent") != "stocks-marketing/1" {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		if calls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/rss+xml")
		_, _ = response.Write(fixture)
	}))
	defer server.Close()

	client := NewClient(server.Client(), Config{
		RSSURL: server.URL + "/api/companyRSS.xml?crpCd=00164779", TargetSymbol: "xyz:SKHY", RequestTimeout: time.Second,
	})
	client.retryDelay = func(int) time.Duration { return 0 }
	selection, err := client.LatestCatalyst(context.Background(), time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || selection.Availability != domain.AvailabilityAvailable || selection.Event.SourceEventID != "20260814802986" {
		t.Fatalf("calls/selection = %d / %#v", calls.Load(), selection)
	}
}

func TestClientLatestCatalystDoesNotRetryPermanentHTTPFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.Client(), Config{
		RSSURL: server.URL, TargetSymbol: "xyz:SKHY", RequestTimeout: time.Second,
	})
	client.retryDelay = func(int) time.Duration { return 0 }
	if _, err := client.LatestCatalyst(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestClientLatestCatalystRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", maxRSSResponseBytes+1)))
	}))
	defer server.Close()

	client := NewClient(server.Client(), Config{
		RSSURL: server.URL, TargetSymbol: "xyz:SKHY", RequestTimeout: time.Second,
	})
	if _, err := client.LatestCatalyst(context.Background(), time.Now()); err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("error = %v", err)
	}
}
