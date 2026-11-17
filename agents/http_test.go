package agents

import (
	"context"
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
	if got, err := TextResponse("p", "text", []string{"a", "b"}); err != nil || got.Text != "a\nb" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := TextResponse("p", "text", nil); err == nil {
		t.Fatal("expected error for no parts")
	}
}
