// Package openai implements agents.Agent using the OpenAI Responses API.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rustyeddy/investor/agents"
)

const (
	provider   = "openai"
	defaultURL = "https://api.openai.com/v1/responses"
)

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
	payload := struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        string `json:"input"`
	}{a.Model, input.Instructions, input.Prompt}
	var decoded struct {
		agents.Envelope
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	headers := map[string]string{"Authorization": "Bearer " + a.Key}
	if err := agents.PostJSON(ctx, a.Client, provider, endpoint, headers, payload, &decoded); err != nil {
		return agents.Response{}, err
	}
	if err := decoded.Err(provider); err != nil {
		return agents.Response{}, err
	}
	// Fail closed: only a completed response is a finished answer.
	if decoded.Status != "completed" {
		reason := decoded.Status
		if decoded.IncompleteDetails != nil && decoded.IncompleteDetails.Reason != "" {
			reason += ": " + decoded.IncompleteDetails.Reason
		}
		if reason == "" {
			return agents.Response{}, errors.New("openai: response missing status")
		}
		return agents.Response{}, fmt.Errorf("openai: incomplete response: status %s", reason)
	}
	var parts []string
	for _, item := range decoded.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" {
				parts = append(parts, c.Text)
			}
		}
	}
	return agents.TextResponse(provider, "output_text", parts)
}
