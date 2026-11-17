// Package openai implements agents.Agent using the OpenAI Responses API.
package openai

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

const defaultURL = "https://api.openai.com/v1/responses"

type Agent struct {
	Key    string
	Model  string
	Client *http.Client
	// Endpoint is intended for local tests and compatible gateways.
	Endpoint string
}

var _ agents.Agent = (*Agent)(nil)

func New(key, model string) *Agent { return &Agent{Key: key, Model: model} }

func (a *Agent) Run(ctx context.Context, input agents.Request) (agents.Response, error) {
	if a.Key == "" || a.Model == "" {
		return agents.Response{}, errors.New("openai: key and model required")
	}
	endpoint := a.Endpoint
	if endpoint == "" {
		endpoint = defaultURL
	}
	payload, err := json.Marshal(struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        string `json:"input"`
	}{a.Model, input.Instructions, input.Prompt})
	if err != nil {
		return agents.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return agents.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Key)
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
		return agents.Response{}, fmt.Errorf("openai: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&decoded); err != nil {
		return agents.Response{}, fmt.Errorf("openai: decode: %w", err)
	}
	if decoded.Error != nil {
		return agents.Response{}, fmt.Errorf("openai: %s", decoded.Error.Message)
	}
	var parts []string
	for _, item := range decoded.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" {
				parts = append(parts, c.Text)
			}
		}
	}
	if len(parts) == 0 {
		return agents.Response{}, errors.New("openai: response has no output_text")
	}
	return agents.Response{Text: strings.Join(parts, "\n")}, nil
}
