package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/agents"
)

// serve returns a client-ready Agent backed by a server that replies with body
// and records the decoded request.
func serve(t *testing.T, status int, body string, got *map[string]any) *Agent {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("x-api-key") != "test" || r.Header.Get("anthropic-version") == "" || r.Header.Get("Content-Type") != "application/json" {
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
	a := serve(t, 200, `{"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":7},"content":[{"type":"text","text":"research"},{"type":"text","text":"result"}]}`, &req)
	a.MaxTokens = 77
	got, err := a.Run(context.Background(), agents.Request{Instructions: "analyst", Prompt: "evaluate"})
	if err != nil || got.Text != "research\nresult" || got.Usage.InputTokens != 11 || got.Usage.OutputTokens != 7 {
		t.Fatalf("Run = %+v, %v", got, err)
	}
	msgs, _ := req["messages"].([]any)
	if req["model"] != "test-model" || req["system"] != "analyst" || req["max_tokens"] != float64(77) || len(msgs) != 1 {
		t.Fatalf("request = %v", req)
	}
	if m := msgs[0].(map[string]any); m["role"] != "user" || m["content"] != "evaluate" {
		t.Fatalf("message = %v", m)
	}
}

func TestRunFailsClosed(t *testing.T) {
	text := `"content":[{"type":"text","text":"partial"}]`
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"max_tokens":     {200, `{"stop_reason":"max_tokens",` + text + `}`, `incomplete response: stop_reason "max_tokens"`},
		"refusal":        {200, `{"stop_reason":"refusal",` + text + `}`, `stop_reason "refusal"`},
		"missing reason": {200, `{` + text + `}`, "missing stop_reason"},
		"no text":        {200, `{"stop_reason":"end_turn","content":[]}`, "no text"},
		"malformed":      {200, `{"stop_reason":`, "decode"},
		"error envelope": {200, `{"error":{"message":"overloaded"}}`, "claude: overloaded"},
		"http error":     {503, `unavailable`, "HTTP 503"},
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
