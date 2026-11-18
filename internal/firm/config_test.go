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
