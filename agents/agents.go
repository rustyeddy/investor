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
	Text string
}
