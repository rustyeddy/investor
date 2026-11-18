package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Envelope is embedded in provider response types to capture an API error
// reported in an otherwise successful HTTP response.
type Envelope struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Err returns the reported API error, if any.
func (e Envelope) Err(provider string) error {
	if e.Error == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", provider, e.Error.Message)
}

// PostJSON sends payload as JSON to url with the given headers and decodes a
// successful response into out. A nil client uses http.DefaultClient. Non-2xx
// statuses return an error that includes a bounded excerpt of the body.
func PostJSON(ctx context.Context, client *http.Client, provider, url string, headers map[string]string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &StatusError{Provider: provider, Status: res.StatusCode, Body: strings.TrimSpace(string(excerpt))}
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: decode: %w", provider, err)
	}
	return nil
}

// StatusError is a non-2xx provider response.
type StatusError struct {
	Provider string
	Status   int
	Body     string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Body)
}

// Retryable reports whether the same request may succeed later: rate limits
// and server-side errors.
func (e *StatusError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Retryable reports whether err is a transient provider or network failure.
// Context cancellation and deadlines are never retryable.
func Retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Retryable()
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF)
}

// TextResponse joins text parts into a Response, or fails if there are none.
func TextResponse(provider, kind string, parts []string, usage Usage) (Response, error) {
	if len(parts) == 0 {
		return Response{}, errors.New(provider + ": response has no " + kind)
	}
	return Response{Text: strings.Join(parts, "\n"), Usage: usage}, nil
}
