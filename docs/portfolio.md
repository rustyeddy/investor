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
  archive.json         what an archive added to the package, and from where
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
3. **Archive** with `investor portfolio archive`: each run whose `run.json` reports
   a terminal status is copied, byte for byte, into the prepared worktree, with
   `experiment.json`, `checksums.sha256` and `archive.json`.
4. **Publish:** commit, push and open the Portfolio PR. Rusty merges.

`<experiment>` is the report's `id`. `investor` does step 3; prepare and publish
are automated in a later PR. Until then:

```sh
git -C /srv/portfolio fetch origin
git -C /srv/portfolio worktree add -b research/<experiment> /srv/portfolio.worktrees/<experiment> origin/main
# run the study (Trader's results.root is /srv/portfolio/.incoming/runs)
investor portfolio archive /srv/portfolio.worktrees/<experiment> <report.json> \
  --path backtests/<strategy>/<INSTRUMENT>/<Interval>/<experiment-dir>/<run-dir>
# add report.md (and notes.org if wanted), commit, push, open the PR
```

## Archive

`investor portfolio archive <worktree> <report.json>` reads Trader's runs from
`<root>/.incoming/runs`, where `--root` is the **main** checkout. It writes only
into `<worktree>`, which must be the top of a linked worktree of that repository
with branch `research/<experiment>` checked out. It commits nothing.

- A new experiment needs `--path`. Its instrument and interval segments must match
  the report, and nothing may be there already.
- A run is copied only when its `run.json` is that run's and says `completed`,
  `failed` or `cancelled`. A run still `running` (a crashed run looks the same),
  or with no readable `run.json`, stops the whole archive.
- A run with no directory in the ingress, or a finished run without a journal, is
  listed as missing, and the package reads `partial`.
- A source file that changes while it is read stops the archive, as does a
  symlink. If no run file at all is found, nothing is written.
- The package is assembled in a `.staging-<experiment>` directory, checked by the
  reader and renamed into place. If an archive is killed part way, see
  [Recovery](#recovery).
- The ingress is left as it was. Clearing it is manual.

`archive.json` (schema version 1) records the experiment id, package path, time,
source directory, whether an existing package was completed and, per run, Trader's
status, each file's SHA-256, size and origin (`copied` or `already_archived`) and
the missing files.

### Completing a partial package

A package merged without its Trader files, such as WFA-02, is completed in a new
worktree from current `main`, on branch `research/<experiment>`, by giving the
same report (the package's own `experiment.json` will do) and no `--path`:

```sh
investor portfolio archive /srv/portfolio.worktrees/<experiment> \
  /srv/portfolio.worktrees/<experiment>/<package>/experiment.json
```

This is the one way an existing package gains files. The report must be byte for
byte the package's `experiment.json`, the package must verify, and it must have no
`archive.json` yet. Existing files are not changed; a copy in the ingress that
differs from an archived file stops the archive. `checksums.sha256` keeps its
lines and gains lines for the added files.

Completion happens only when the result reads `complete`: every run attempt in
the manifest with all three of Trader's files. If any is lacking, the command says
which and leaves the package unchanged, so it can be run again when they arrive.
There is no override. A manifest with an attempt that never got a RunID therefore
cannot be completed. Once a package has `archive.json` it is closed, including a
new experiment that was archived partial.

The run files must be in the ingress. Runs written elsewhere, as WFA-02's were,
are first copied there unchanged (`cp -a <results-root>/run_... <root>/.incoming/runs/`).

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

### Recovery

An archive killed part way can leave directories beside the package, in its parent
directory. They are ignored by Git, so look with `ls -a`:

| Left behind | Package directory | What to do |
|---|---|---|
| `.staging-<experiment>` only | as before (absent for a new experiment) | remove the staging directory |
| `.staging-<experiment>.old` and `.staging-<experiment>` | **absent** | `.old` is the package as it was: `mv .staging-<experiment>.old <run-dir>`, then remove `.staging-<experiment>` |
| `.staging-<experiment>.old` only | present | the completed package is in place: check it with `investor portfolio --root <worktree> summary <experiment>` (evidence `complete`), then remove `.old` |

Never remove `.old` while the package directory is absent: it may be the only copy
of uncommitted files such as `notes.org`. Then run the archive again if the
package is not complete. `archive` refuses to run while `.old` exists.

## Reading the repository

`list`, `summary`, `cat` and `reconcile` never write. `--root` (or
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
