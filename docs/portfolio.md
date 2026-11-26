# Portfolio repository

The Portfolio repository (`rustyeddy/portfolio`, checked out at `/srv/portfolio`)
is the source of truth for backtest runs and studies once their evidence is
finalized and archived. [AGENTS.md](../AGENTS.md#investor-and-portfolio) holds
the rules; this page holds the layout and the commands. Tracked in
[Investor #48](https://github.com/rustyeddy/investor/issues/48).

## Layout

```
backtests/<strategy>/<INSTRUMENT>/<Interval>/<experiment>/<run>/
  experiment.json      Investor's report (wfa.Report), byte for byte
  report.md            human summary of the result (not checksummed)
  checksums.sha256     sha256sum format, relative paths, every file but itself, report.md and notes.org
  archive.json         archive record (planned, PR 2)
  notes.org            questions, observations, hypotheses (editable)
  runs/<RunID>/
    run.json           Trader's run record
    events.jsonl       Trader's event journal
    account.jsonl      Trader's account journal
```

- The **experiment id** is the `id` field of `experiment.json`, for example
  `wfa01-emax-eurusd-h1-run1`. It is unique across the repository, and it is not
  the path. Duplicate ids make every package that carries them invalid.
- A **RunID** is Trader's `run_` followed by a 26-character ULID. Every run
  attempt in the manifest (training runs, then the OOS run) should name one, and
  its directory is `runs/<RunID>/`. A run directory the manifest does not name is
  a problem. An attempt that ended before Trader assigned a RunID is still
  counted, as a run with no RunID and `missing` evidence, so its experiment can
  never read complete.
- Trader writes runs to `/srv/portfolio/.incoming/runs` (its `results.root`).
  That directory, and `.staging-*` directories used while archiving, are
  gitignored and skipped by the reader. Directories whose names start with a dot
  are never read as packages.

## Three statuses, kept apart

- **Experiment status:** the report's `status` (`complete`,
  `no_eligible_selection`, ...), as Investor recorded it.
- **Run status:** Trader's `run.json` status, which must be terminal:
  `completed`, `failed` or `cancelled`. A failed or cancelled run can be
  complete evidence and stays labeled failed or cancelled. Any other status,
  `running` or empty included, is not evidence, and the run reads invalid.
- **Evidence:** whether the files are there and match `checksums.sha256`:
  - `complete`: `run.json`, `events.jsonl` and `account.jsonl` all verified;
  - `partial`: some verified, the rest absent;
  - `missing`: none present;
  - `invalid`: a file is unlisted, differs from its checksum, is not a regular
    file (a symlink, for example), or the package has a problem.

An experiment's evidence is `invalid` if the manifest is not verified, any run is
invalid or the package has a problem, `missing` if the manifest lists no runs,
`partial` if any run is not complete, and otherwise `complete`. The historical WFA-01 and WFA-02 packages read `partial`:
they predate `account.jsonl`, and WFA-02 has no Trader files at all.

## Workflow

One branch, worktree and PR per experiment, in this order:

1. **Prepare**, before the run: a branch `research/<experiment>` and a worktree
   for it in `/srv/portfolio.worktrees/<experiment>`. The main checkout is never
   switched or written.
2. **Run** the study with Trader writing to `.incoming/runs`.
3. **Archive** each run whose `run.json` reports a terminal status, byte for
   byte, into the prepared worktree, with `experiment.json`, `checksums.sha256`
   and `archive.json`.
4. **Publish:** commit, push and open the Portfolio PR. Rusty merges.

`investor` automates prepare, archive and publish in later PRs. Until then, do
them by hand:

```sh
git -C /srv/portfolio fetch origin
git -C /srv/portfolio worktree add -b research/<experiment> /srv/portfolio.worktrees/<experiment> origin/main
# run; copy finalized runs into the package; then, inside the package:
find . -type f ! -name checksums.sha256 ! -name report.md ! -name notes.org -printf '%P\0' | sort -z | xargs -0 sha256sum > checksums.sha256
```

## Notes

`notes.org` is Emacs Org mode, started from
[notes-template.org](notes-template.org). It is the place for observations,
questions, hypotheses and follow-up; the evidence files never change, and
`report.md` states the result, not ideas. Edit `notes.org` in the experiment's open PR, or later in a notes-only PR. Tie
each point to the evidence it rests on (a RunID, a file, a figure), and label a
hypothesis as one until evidence supports it:

```org
* Questions
** Why did OOS (run_01M8600S0W2PX33XJXVGXV770B) beat every training window?
* Hypotheses
** HYPOTHESIS: 2015 trended more than the training years
   Untested. Compare ADX by year before acting on it.
```

## Reading the repository

`investor portfolio` reads and never writes. `--root` (or
`INVESTOR_PORTFOLIO_ROOT`, default `/srv/portfolio`) points at the checkout or a
worktree. Every file is opened through a root that cannot escape it, must be a
regular file, and is hashed against `checksums.sha256` before it is used.

```sh
investor portfolio list                                    # experiments, evidence, run counts, problems
investor portfolio summary <experiment-id> [run-id]        # JSON, with each file's state
investor portfolio cat <experiment-id> <run-id> run.json   # one verified Trader file
investor portfolio reconcile <experiment-id> <run-id>      # account journal against the recorded summary
```

`cat` and `reconcile` refuse a file that is missing, unlisted or changed, and
refuse every file of a package whose manifest does not verify.
