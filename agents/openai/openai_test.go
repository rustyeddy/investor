package openai

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
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected request: %s %s", r.Method, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"research result"}]}]}`))
	}))
	defer server.Close()
	a := New("test", "test-model")
	a.Endpoint, a.Client = server.URL, server.Client()
	got, err := a.Run(context.Background(), agents.Request{Instructions: "analyst", Prompt: "evaluate"})
	if err != nil || got.Text != "research result" {
		t.Fatalf("Run = %+v, %v", got, err)
	}
}

func TestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
	}))
	defer server.Close()
	a := New("test", "model")
	a.Endpoint = server.URL
	_, err := a.Run(context.Background(), agents.Request{Prompt: "test"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected 429 error, got %v", err)
	}
}
