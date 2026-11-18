// Package agents defines the provider-neutral contract for text model calls.
// Investor's workflow engine, not the model provider, owns tool authorization.
package agents

import "context"

type Agent interface {
	Run(context.Context, Request) (Response, error)
}

type Request struct {
	Instructions string
	Prompt       string
}

type Response struct {
	Text  string
	Usage Usage
}

// Usage is the token usage a provider reported for one call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Total returns input plus output tokens.
func (u Usage) Total() int { return u.InputTokens + u.OutputTokens }
