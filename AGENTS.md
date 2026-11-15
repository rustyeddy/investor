# Investor — Development Instructions

These instructions apply to all work in this repository. Read [docs/development.org](docs/development.org) before starting GitHub issues, branches, PRs, or reviews.

## Principles

- Prefer simple, idiomatic Go, few packages, and minimal dependencies.
- Keep Investor independent from Trader and Tulip.
- Make the smallest change that satisfies the assigned issue; propose unrelated work separately.
- Preserve research evidence, timestamps, and paper-trading safety boundaries.
- Update tests and documentation when behavior changes.

## Git and review rules

- Start from an issue with explicit acceptance criteria; ask if the scope is unclear.
- Create a dedicated branch from current `main`. Never commit directly to `main`.
- Open a focused PR linked to the issue. Do not merge your own PR without Rusty's explicit approval.
- Run `go test -race ./...` and `go vet ./...` for Go changes; report failures and any checks not run.
- Review correctness, simplicity, boundaries, tests, and documentation. Address feedback before requesting approval.
- Do not force-push `main`, bypass protected workflows, or silently change trading risk policy.
- If permissions or required approval are missing, stop and ask.

## Pull Request Completion

- Every PR must reference its related GitHub issue.
- Use `Closes #N` in the PR description when the PR
  fully resolves the issue.
- Use `Refs #N` for partial work.
- Merge only after required reviews and tests pass.
- GitHub automatically closes linked issues on merge.
- Enable automatic deletion of merged feature branches
  in repository settings.
- Never automatically merge without required approval.

## Documentation ownership

- `README.md`: concise purpose, current capabilities, and development phases.
- `docs/development.org`: detailed issue → branch → PR → review → merge procedure.
- `roles/` and `skills/`: Investor trading-agent behavior, not Codex development rules.
- `skills/<name>/SKILL.md`: standard on-disk skill layout.
