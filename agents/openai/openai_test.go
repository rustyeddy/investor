package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/agents"
)

// serve returns an Agent backed by a server that replies with body and
// records the decoded request.
func serve(t *testing.T, status int, body string, got *map[string]any) *Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %v", r.Method, r.Header)
		}
		if got != nil {
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
				t.Errorf("request body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	a := New("test", "test-model")
	a.Endpoint, a.Client = server.URL, server.Client()
	return a
}

func TestRunSendsRequestAndReturnsText(t *testing.T) {
	var req map[string]any
	a := serve(t, 200, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"research"},{"type":"output_text","text":"result"}]}]}`, &req)
	got, err := a.Run(context.Background(), agents.Request{Instructions: "analyst", Prompt: "evaluate"})
	if err != nil || got.Text != "research\nresult" {
		t.Fatalf("Run = %+v, %v", got, err)
	}
	if req["model"] != "test-model" || req["instructions"] != "analyst" || req["input"] != "evaluate" {
		t.Fatalf("request = %v", req)
	}
}

func TestRunFailsClosed(t *testing.T) {
	out := `"output":[{"content":[{"type":"output_text","text":"partial"}]}]`
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"incomplete":     {200, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},` + out + `}`, "incomplete response: status incomplete: max_output_tokens"},
		"failed":         {200, `{"status":"failed",` + out + `}`, "status failed"},
		"missing status": {200, `{` + out + `}`, "missing status"},
		"no text":        {200, `{"status":"completed","output":[{"content":[{"type":"refusal","text":"no"}]}]}`, "no output_text"},
		"malformed":      {200, `{"status":`, "decode"},
		"error envelope": {200, `{"error":{"message":"overloaded"}}`, "openai: overloaded"},
		"http error":     {429, `rate limit`, "HTTP 429"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := serve(t, tc.status, tc.body, nil).Run(context.Background(), agents.Request{Prompt: "test"})
			if err == nil || !strings.Contains(err.Error(), tc.want) || got.Text != "" {
				t.Fatalf("got %+v, %v; want error containing %q", got, err, tc.want)
			}
		})
	}
}

func TestRunHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := serve(t, 200, `{}`, nil).Run(ctx, agents.Request{Prompt: "test"})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequiresKeyAndModel(t *testing.T) {
	if _, err := New("", "m").Run(context.Background(), agents.Request{}); err == nil {
		t.Fatal("expected error for missing key")
	}
	if _, err := New("k", "").Run(context.Background(), agents.Request{}); err == nil {
		t.Fatal("expected error for missing model")
	}
}
