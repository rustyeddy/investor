package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		return fmt.Errorf("%s: HTTP %d: %s", provider, res.StatusCode, strings.TrimSpace(string(excerpt)))
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("%s: decode: %w", provider, err)
	}
	return nil
}

// TextResponse joins text parts into a Response, or fails if there are none.
func TextResponse(provider, kind string, parts []string) (Response, error) {
	if len(parts) == 0 {
		return Response{}, errors.New(provider + ": response has no " + kind)
	}
	return Response{Text: strings.Join(parts, "\n")}, nil
}
