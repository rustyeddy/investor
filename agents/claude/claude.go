// Package claude implements agents.Agent using Anthropic's Messages API.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rustyeddy/investor/agents"
)

const defaultURL = "https://api.anthropic.com/v1/messages"

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
	payload, err := json.Marshal(struct {
		Model     string              `json:"model"`
		System    string              `json:"system,omitempty"`
		MaxTokens int                 `json:"max_tokens"`
		Messages  []map[string]string `json:"messages"`
	}{a.Model, input.Instructions, maxTokens, []map[string]string{{"role": "user", "content": input.Prompt}}})
	if err != nil {
		return agents.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return agents.Response{}, err
	}
	req.Header.Set("x-api-key", a.Key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return agents.Response{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return agents.Response{}, fmt.Errorf("claude: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&decoded); err != nil {
		return agents.Response{}, fmt.Errorf("claude: decode: %w", err)
	}
	if decoded.Error != nil {
		return agents.Response{}, fmt.Errorf("claude: %s", decoded.Error.Message)
	}
	var parts []string
	for _, c := range decoded.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return agents.Response{}, errors.New("claude: response has no text")
	}
	return agents.Response{Text: strings.Join(parts, "\n")}, nil
}
