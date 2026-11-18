package firm

import (
	"strings"
	"testing"
)

func TestLLMConfigValidation(t *testing.T) {
	base := `"agents":[{"id":"a","role":"Market Analyst"}],"workflows":[{"id":"w","agents":["a"]}]`
	for name, tc := range map[string]struct{ extra, want string }{
		"valid default":    {`"llm":{"provider":"claude","model":"m"},`, ""},
		"valid role":       {`"roles":{"Market Analyst":{"provider":"openai","model":"m","api_key_env":"K"}},`, ""},
		"unknown provider": {`"llm":{"provider":"acme","model":"m"},`, "unknown llm provider"},
		"missing model":    {`"llm":{"provider":"claude"},`, "model is required"},
		"negative timeout": {`"llm":{"provider":"claude","model":"m","timeout_seconds":-1},`, "must not be negative"},
		"unknown role":     {`"roles":{"Nobody":{"provider":"claude","model":"m"}},`, `no agent has role "Nobody"`},
		"inline secret":    {`"llm":{"provider":"claude","model":"m","api_key":"sk-x"},`, "unknown field"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader("{" + tc.extra + base + "}"))
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSafeguardConfigValidation(t *testing.T) {
	base := `"agents":[{"id":"a","role":"R"}],"workflows":[{"id":"w","agents":["a"]}]`
	for name, tc := range map[string]struct{ extra, want string }{
		"valid": {`"limits":{"max_calls_per_run":5,"max_tokens_per_run":1000,"max_cost_usd_per_run":1.5},` +
			`"llm":{"provider":"claude","model":"m","max_retries":0,"input_usd_per_mtok":3,"output_usd_per_mtok":15},`, ""},
		"negative limit":   {`"limits":{"max_calls_per_run":-1},`, "limits must not be negative"},
		"too many retries": {`"llm":{"provider":"claude","model":"m","max_retries":6},`, "max_retries must be between"},
		"negative retries": {`"llm":{"provider":"claude","model":"m","max_retries":-1},`, "max_retries must be between"},
		"negative price":   {`"llm":{"provider":"claude","model":"m","input_usd_per_mtok":-1},`, "must not be negative"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader("{" + tc.extra + base + "}"))
			switch {
			case tc.want == "" && err != nil:
				t.Fatal(err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
