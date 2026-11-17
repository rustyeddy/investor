// Package claude implements agents.Agent using Anthropic's Messages API.
package claude

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rustyeddy/investor/agents"
)

const (
	provider   = "claude"
	defaultURL = "https://api.anthropic.com/v1/messages"
)

type Agent struct {
	Key       string
	Model     string
	Client    *http.Client
	Endpoint  string // For local tests and compatible gateways.
	MaxTokens int
}

var _ agents.Agent = (*Agent)(nil)

func New(key, model string) *Agent { return &Agent{Key: key, Model: model, MaxTokens: 1024} }

func (a *Agent) Run(ctx context.Context, input agents.Request) (agents.Response, error) {
	if a.Key == "" || a.Model == "" {
		return agents.Response{}, errors.New("claude: key and model required")
	}
	endpoint := a.Endpoint
	if endpoint == "" {
		endpoint = defaultURL
	}
	maxTokens := a.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	payload := struct {
		Model     string              `json:"model"`
		System    string              `json:"system,omitempty"`
		MaxTokens int                 `json:"max_tokens"`
		Messages  []map[string]string `json:"messages"`
	}{a.Model, input.Instructions, maxTokens, []map[string]string{{"role": "user", "content": input.Prompt}}}
	var decoded struct {
		agents.Envelope
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	headers := map[string]string{"x-api-key": a.Key, "anthropic-version": "2023-06-01"}
	if err := agents.PostJSON(ctx, a.Client, provider, endpoint, headers, payload, &decoded); err != nil {
		return agents.Response{}, err
	}
	if err := decoded.Err(provider); err != nil {
		return agents.Response{}, err
	}
	// Fail closed: only a natural stop is a complete answer. "max_tokens" is
	// truncated; "refusal", "pause_turn" and "tool_use" are not final text.
	switch decoded.StopReason {
	case "end_turn", "stop_sequence":
	case "":
		return agents.Response{}, errors.New("claude: response missing stop_reason")
	default:
		return agents.Response{}, fmt.Errorf("claude: incomplete response: stop_reason %q", decoded.StopReason)
	}
	var parts []string
	for _, c := range decoded.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return agents.TextResponse(provider, "text", parts)
}
