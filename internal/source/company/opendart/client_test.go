package opendart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetRetriesTransientFailureWithoutExposingCredential(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/list.json" || request.URL.Query().Get("crtfc_key") != strings.Repeat("k", 40) {
			t.Fatalf("unexpected OpenDART request")
		}
		if calls.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte(`{"status":"000"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), Config{BaseURL: server.URL, APIKey: strings.Repeat("k", 40), RequestTimeout: time.Second})
	client.retryDelay = func(int) time.Duration { return 0 }
	raw, hash, err := client.get(context.Background(), "list.json", url.Values{}, 1024)
	if err != nil || calls.Load() != 2 || len(raw) == 0 || len(hash) != 64 {
		t.Fatalf("calls/raw/hash/err = %d/%q/%q/%v", calls.Load(), raw, hash, err)
	}
}

func TestGetDoesNotRetryPermanentFailureOrExposeCredential(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	credential := strings.Repeat("k", 40)
	client := NewClient(server.Client(), Config{BaseURL: server.URL, APIKey: credential, RequestTimeout: time.Second})
	client.retryDelay = func(int) time.Duration { return 0 }
	_, _, err := client.get(context.Background(), "list.json", url.Values{}, 1024)
	if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), credential) {
		t.Fatalf("calls/error = %d/%v", calls.Load(), err)
	}
}
