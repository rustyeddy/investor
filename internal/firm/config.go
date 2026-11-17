// Package firm defines and runs the firm's agent workflows independently of
// its CLI, HTTP transport, model provider, and trading infrastructure.
package firm

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

type Agent struct {
	ID     string   `json:"id"`
	Role   string   `json:"role"`
	Skills []string `json:"skills"`
	Tools  []string `json:"tools"`
}

type Skill struct {
	ID           string `json:"id"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// Tool is a capability declaration, not executable code. An adapter must
// register an implementation before a runtime can invoke it.
type Tool struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Workflow struct {
	ID     string   `json:"id"`
	Agents []string `json:"agents"`
}

// LLM selects the model provider and model a role runs on. Credentials are
// never stored in configuration: APIKeyEnv names the environment variable that
// holds the key (default OPENAI_API_KEY or ANTHROPIC_API_KEY).
type LLM struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	APIKeyEnv      string `json:"api_key_env,omitempty"`
	MaxTokens      int    `json:"max_tokens,omitempty"`      // Claude only.
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"` // Per model call; default 60.
}

// Providers are the model providers Investor can run roles on.
var Providers = []string{"claude", "openai"}

func (l LLM) validate(where string) error {
	if !slices.Contains(Providers, l.Provider) {
		return fmt.Errorf("%s: unknown llm provider %q (want one of %v)", where, l.Provider, Providers)
	}
	if strings.TrimSpace(l.Model) == "" {
		return fmt.Errorf("%s: llm model is required", where)
	}
	if l.MaxTokens < 0 || l.TimeoutSeconds < 0 {
		return fmt.Errorf("%s: llm max_tokens and timeout_seconds must not be negative", where)
	}
	return nil
}

type Config struct {
	SkillsDir string `json:"skills_dir,omitempty"`
	// LLM is the default model for roles without an entry in Roles. Both are
	// used only by the model runtime; the dry-run runtime ignores them.
	LLM       *LLM           `json:"llm,omitempty"`
	Roles     map[string]LLM `json:"roles,omitempty"` // Agent role name -> LLM.
	Agents    []Agent        `json:"agents"`
	Skills    []Skill        `json:"skills"`
	Tools     []Tool         `json:"tools"`
	Workflows []Workflow     `json:"workflows"`
}

func Decode(r io.Reader) (Config, error) {
	c, err := decode(r)
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

func decode(r io.Reader) (Config, error) {
	var c Config
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("expected a single JSON configuration")
	}
	return c, nil
}

func (c Config) Validate() error {
	agents, skills, tools, workflows := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	add := func(m map[string]bool, kind, id string) error {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
			return fmt.Errorf("invalid %s id %q", kind, id)
		}
		if m[id] {
			return fmt.Errorf("duplicate %s %q", kind, id)
		}
		m[id] = true
		return nil
	}
	for _, s := range c.Skills {
		if !validSkillID(s.ID) {
			return fmt.Errorf("invalid skill id %q", s.ID)
		}
		if err := add(skills, "skill", s.ID); err != nil {
			return err
		}
		if strings.TrimSpace(s.Instructions) == "" {
			return fmt.Errorf("skill %q has no instructions", s.ID)
		}
	}
	for _, t := range c.Tools {
		if err := add(tools, "tool", t.ID); err != nil {
			return err
		}
	}
	for _, a := range c.Agents {
		if err := add(agents, "agent", a.ID); err != nil {
			return err
		}
		if strings.TrimSpace(a.Role) == "" {
			return fmt.Errorf("agent %q has no role", a.ID)
		}
		for _, id := range a.Skills {
			if !skills[id] {
				return fmt.Errorf("agent %q references unknown skill %q", a.ID, id)
			}
		}
		for _, id := range a.Tools {
			if !tools[id] {
				return fmt.Errorf("agent %q references unknown tool %q", a.ID, id)
			}
		}
	}
	if c.LLM != nil {
		if err := c.LLM.validate("llm"); err != nil {
			return err
		}
	}
	for role, l := range c.Roles {
		if !slices.ContainsFunc(c.Agents, func(a Agent) bool { return a.Role == role }) {
			return fmt.Errorf("roles: no agent has role %q", role)
		}
		if err := l.validate(fmt.Sprintf("roles[%q]", role)); err != nil {
			return err
		}
	}
	if len(c.Workflows) == 0 {
		return fmt.Errorf("at least one workflow is required")
	}
	for _, w := range c.Workflows {
		if err := add(workflows, "workflow", w.ID); err != nil {
			return err
		}
		if len(w.Agents) == 0 {
			return fmt.Errorf("workflow %q has no agents", w.ID)
		}
		for _, id := range w.Agents {
			if !agents[id] {
				return fmt.Errorf("workflow %q references unknown agent %q", w.ID, id)
			}
		}
	}
	return nil
}
