package transports

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/internal/firm"
)

func TestAPI(t *testing.T) {
	c := firm.Config{Agents: []firm.Agent{{ID: "a", Role: "research"}}, Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a"}}}}
	e, err := firm.New(c, firm.DryRun{}, nil, "dry-run")
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(c, e)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/health", "", 200},
		{"GET", "/api/manager/agents", "", 200},
		{"POST", "/api/manager/runs", `{"workflow":"w","objective":"test"}`, 200},
		{"POST", "/api/manager/runs", `{"workflow":"missing"}`, 404},
		{"POST", "/api/manager/runs", `{"workflow":"w","unknown":true}`, 400},
		{"POST", "/api/manager/runs", `{"workflow":"w"} {}`, 400},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.body, w.Code, w.Body.String())
		}
		if tc.method == "POST" && tc.status == 200 {
			var run firm.Run
			if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
				t.Fatal(err)
			}
			if run.Mode != "dry-run" || run.Status != "succeeded" || len(run.Results) != 1 {
				t.Fatal(run)
			}
		}
	}
}
