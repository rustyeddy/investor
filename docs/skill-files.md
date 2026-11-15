# File-backed skills

Investor loads procedures from `skills/<id>/SKILL.md`. IDs use lowercase letters,
digits and single hyphens, start/end with a letter or digit, and are at most
63 characters. The frontmatter `name` must equal the directory/configuration ID.
Each file requires YAML frontmatter with nonempty string `name` and `description`,
followed by a nonempty Markdown body. Additional YAML metadata is permitted.

```markdown
---
name: market-overview
description: Summarize timestamped market observations for a research session.
---
Instructions go here.
```

Configuration declares the skill catalog by ID; agents refer to those IDs:

```json
{
  "skills_dir": "../skills",
  "skills": [{"id": "market-overview"}],
  "agents": [{"id": "analyst", "role": "Market Analyst", "skills": ["market-overview"], "tools": []}],
  "workflows": [{"id": "research", "agents": ["analyst"]}]
}
```

`skills_dir` defaults to `skills` beside the config file. Relative directories
are resolved against that file's directory, never the process working directory;
absolute roots are supported. The example at `examples/firm.json` uses
`../skills`. Package that directory with the config when moving an installation.
Only catalog entries are loaded; unused directories are ignored. File access is
confined to the selected root, including symlink resolution.

All commands load and validate skills before proceeding, including `validate`,
`plan`, `run` and `serve`. Missing files, malformed frontmatter, mismatched names,
empty descriptions/bodies, invalid IDs and unknown agent skill references fail
with contextual errors. YAML errors are not silently treated as Markdown.
The body becomes the runtime instructions; the description accompanies it in
the skill catalog. Loading happens once at startup, with no hot reload.

Legacy `{"id":"...","instructions":"..."}` catalog entries remain supported
but are deprecated. Nonempty inline instructions take precedence for that entry
and bypass disk loading; do not combine the two representations. The example
contains no inline prompts. In-process callers may still build resolved skills
directly; `firm.Load(path)` is the file-aware entry point, while `firm.Decode`
continues to accept already-resolved/legacy JSON without filesystem access.

The six initial skills cover overview, opportunity discovery, setup analysis,
thesis challenge, advisory risk assessment and performance review. They follow
the [role contracts](../roles/README.md) and
[paper policy](paper-trading-policy.md). They do not grant tools, activate policy,
run models, fetch data or enforce numeric limits. The sample workflow is a
dry-run procedure preview, not an implemented research or trading session.

Verification covers file parsing, safe ID/path handling, loading from another
working directory, legacy input, runtime handoff, CLI validation/dry-run and
service skill output. Go race tests and vet remain the required checks.
