package agents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("X-Test") != "1" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %v", r.Method, r.Header)
		}
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer server.Close()
	var out Envelope
	if err := PostJSON(context.Background(), server.Client(), "p", server.URL, map[string]string{"X-Test": "1"}, map[string]string{"a": "b"}, &out); err != nil {
		t.Fatal(err)
	}
	if err := out.Err("p"); err == nil || err.Error() != "p: boom" {
		t.Fatalf("Err = %v", err)
	}
}

func TestPostJSONStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()
	err := PostJSON(context.Background(), nil, "p", server.URL, nil, struct{}{}, new(Envelope))
	if err == nil || !strings.Contains(err.Error(), "p: HTTP 502: nope") {
		t.Fatalf("got %v", err)
	}
}

func TestTextResponse(t *testing.T) {
	if got, err := TextResponse("p", "text", []string{"a", "b"}, Usage{InputTokens: 2, OutputTokens: 3}); err != nil || got.Text != "a\nb" || got.Usage.Total() != 5 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := TextResponse("p", "text", nil, Usage{}); err == nil {
		t.Fatal("expected error for no parts")
	}
}

func TestRetryable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&StatusError{Status: 429}, true},
		{&StatusError{Status: 503}, true},
		{&StatusError{Status: 529}, true},
		{&StatusError{Status: 400}, false},
		{&StatusError{Status: 401}, false},
		{context.DeadlineExceeded, false},
		{context.Canceled, false},
		{io.ErrUnexpectedEOF, true},
		{errors.New("other"), false},
	} {
		if got := Retryable(tc.err); got != tc.want {
			t.Errorf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
