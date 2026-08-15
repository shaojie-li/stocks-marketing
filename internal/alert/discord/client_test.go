package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendDoesNotLeakWebhookTokenInError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	_, err := NewClient(server.Client()).Send(
		context.Background(),
		server.URL+"/api/webhooks/secret-token",
		"delivery-key",
		[]byte(`{"rule_version":"v1","scores":{"trend":{"value":1,"direction":"UP"},"entry":{"value":2,"direction":"DOWN"}}}`),
	)
	if err == nil {
		t.Fatal("Send() succeeded for HTTP 500")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("Send() leaked webhook URL: %v", err)
	}
}
