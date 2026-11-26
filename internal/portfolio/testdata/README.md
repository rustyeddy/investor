`portfolio/` holds real Portfolio packages copied byte for byte from
rustyeddy/portfolio at commit ecc82cf, so the reader is tested against the
historical formats:

- WFA-01 run1: `experiment.json`, `checksums.sha256` and one run
  (`run_01M8600S0W2PX33XJXVGXV770B`, the OOS run: `run.json` and `events.jsonl`).
  The other eight runs are left out, so they read as missing; no run has an
  `account.jsonl`.
- WFA-02 run1: `experiment.json`, `checksums.sha256` and `trader-config.yml`,
  the whole partial package as merged.

Do not edit these files: the checksums would no longer match.
