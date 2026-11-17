package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/agents"
)

func TestRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("x-api-key") != "test" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("unexpected request headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","content":[{"type":"text","text":"research result"}]}`))
	}))
	defer server.Close()
	a := New("test", "test-model")
	a.Endpoint, a.Client = server.URL, server.Client()
	got, err := a.Run(context.Background(), agents.Request{Instructions:"analyst",Prompt:"evaluate"})
	if err != nil || got.Text != "research result" { t.Fatalf("Run = %+v, %v", got, err) }
}

func TestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	a := New("test", "model")
	a.Endpoint = server.URL
	_, err := a.Run(context.Background(), agents.Request{Prompt:"test"})
	if err == nil || !strings.Contains(err.Error(), "503") { t.Fatalf("expected 503 error, got %v", err) }
}
