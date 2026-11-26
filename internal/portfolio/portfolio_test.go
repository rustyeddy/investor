package portfolio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/investor/internal/wfa"
)

const pkgPath = "backtests/emax/EURUSD/H1/wfa09/run1"

func runID(n int) string { return fmt.Sprintf("run_01M8600S0W2PX33XJXVGXV77%02d", n) }

// fixture is an experiment package written under a temporary Portfolio root,
// with every original file listed in checksums.sha256.
type fixture struct {
	t    *testing.T
	root string
	dir  string // the package directory
}

func usd(amount string) wfa.Money { return wfa.Money{Amount: amount, Currency: "USD"} }

// accountJournal is a valid account.jsonl and the summary that reconciles with it.
func accountJournal(t *testing.T) ([]byte, wfa.Summary) {
	t.Helper()
	start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return start.Add(time.Duration(h) * time.Hour) }
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i, amount := range []string{"100", "80", "120"} {
		kind := "frame"
		if i == 0 {
			kind = "initial"
		}
		p := wfa.EquityPoint{Seq: i + 1, Time: at(i), Kind: kind, Account: "sim",
			Balance: usd(amount), Unrealized: usd("0"), Equity: usd(amount)}
		if err := enc.Encode(p); err != nil {
			t.Fatal(err)
		}
	}
	ret, unrealized := "20", usd("0")
	fall := &wfa.Drawdown{Amount: usd("20"), Pct: "20", PeakEquity: usd("100"), TroughEquity: usd("80"), PeakTime: at(0), TroughTime: at(1)}
	return buf.Bytes(), wfa.Summary{Account: "sim", InitialBalance: usd("100"), Balance: usd("120"), Equity: usd("120"),
		Unrealized: &unrealized, NetReturnPct: &ret, MaxDrawdown: fall, MaxPercentDrawdown: fall}
}

// newFixture writes a completed experiment with two training runs and one OOS run.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir()}
	f.dir = filepath.Join(f.root, filepath.FromSlash(pkgPath))
	journal, sum := accountJournal(t)
	rep := wfa.Report{ID: "wfa09-emax-eurusd-h1-run1", Status: wfa.StatusComplete,
		Spec: wfa.Spec{Instrument: "EURUSD", Interval: "h1"}}
	for i := 1; i <= 3; i++ {
		run := wfa.Run{Phase: wfa.PhaseTrain, Params: wfa.Params{Short: 5, Long: 20 * i, Stop: "0.01"},
			Outcome: &wfa.Outcome{RunID: runID(i), Status: wfa.StatusCompleted, Summary: &sum}}
		if i == 3 {
			run.Phase = wfa.PhaseTest
			rep.OOS = &run
		} else {
			rep.Training = append(rep.Training, run)
		}
		f.write(filepath.Join(RunsDir, runID(i), RunFile), fmt.Sprintf(`{"run_id":%q,"kind":"backtest","status":"completed","result":{"trades":3}}`, runID(i)))
		f.write(filepath.Join(RunsDir, runID(i), EventsFile), `{"seq":1,"state":"filled"}`+"\n")
		f.write(filepath.Join(RunsDir, runID(i), AccountFile), string(journal))
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	f.write(ManifestFile, string(data))
	f.sign()
	return f
}

func (f *fixture) write(name, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) remove(name string) {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.dir, name)); err != nil {
		f.t.Fatal(err)
	}
}

// sign rewrites checksums.sha256 for every file in the package except the
// checksum file itself and notes.
func (f *fixture) sign() {
	f.t.Helper()
	var lines []string
	err := filepath.WalkDir(f.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(f.dir, p)
		rel = filepath.ToSlash(rel)
		if rel == ChecksumFile || rel == NotesFile {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, hex.EncodeToString(sum[:])+"  "+rel)
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	sort.Strings(lines)
	f.write(ChecksumFile, strings.Join(lines, "\n")+"\n")
}

func (f *fixture) repo() *Repo {
	f.t.Helper()
	return openRepo(f.t, f.root)
}

func openRepo(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func (f *fixture) only() Experiment {
	f.t.Helper()
	all, err := f.repo().List()
	if err != nil {
		f.t.Fatal(err)
	}
	if len(all) != 1 {
		f.t.Fatalf("List found %d experiments, want 1: %+v", len(all), all)
	}
	return all[0]
}

func runByID(t *testing.T, e Experiment, id string) Run {
	t.Helper()
	run, err := e.run(id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCompleteExperimentWithSeveralRuns(t *testing.T) {
	f := newFixture(t)
	e := f.only()
	if e.ID != "wfa09-emax-eurusd-h1-run1" || e.Path != pkgPath || e.Status != wfa.StatusComplete ||
		e.Strategy != "emax" || e.Instrument != "EURUSD" || e.Interval != "h1" {
		t.Fatalf("experiment = %+v", e)
	}
	if e.Evidence != Complete || e.Manifest != Verified || len(e.Problems) != 0 || len(e.Runs) != 3 {
		t.Fatalf("evidence = %s, manifest = %s, problems = %v, runs = %d", e.Evidence, e.Manifest, e.Problems, len(e.Runs))
	}
	if e.Notes || e.Archived {
		t.Fatalf("notes = %v, archive record = %v, want neither", e.Notes, e.Archived)
	}
	for i, run := range e.Runs {
		if run.RunID != runID(i+1) || run.Evidence != Complete || run.Record == nil || run.Record.Status != "completed" ||
			run.ReportedStatus != wfa.StatusCompleted || run.Reported == nil {
			t.Fatalf("run %d = %+v", i, run)
		}
	}
	if e.Runs[2].Phase != wfa.PhaseTest || e.Runs[0].Phase != wfa.PhaseTrain {
		t.Fatalf("phases = %s, %s", e.Runs[0].Phase, e.Runs[2].Phase)
	}
}

func TestOpenArtifactReturnsTheVerifiedBytes(t *testing.T) {
	f := newFixture(t)
	r := f.repo()
	for _, name := range Artifacts {
		rc, err := r.OpenArtifact("wfa09-emax-eurusd-h1-run1", runID(2), name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		want, _ := os.ReadFile(filepath.Join(f.dir, RunsDir, runID(2), name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: read %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestReconcileUsesTheArchivedJournal(t *testing.T) {
	f := newFixture(t)
	curve, err := f.repo().Reconcile("wfa09-emax-eurusd-h1-run1", runID(3))
	if err != nil || len(curve) != 3 {
		t.Fatalf("curve = %+v, err = %v", curve, err)
	}
}

func TestNotesDoNotAffectEvidence(t *testing.T) {
	f := newFixture(t)
	f.write(NotesFile, "* Questions\n")
	e := f.only()
	f.write(NotesFile, "* Questions\n** Why did OOS improve?\n")
	if again := f.only(); !e.Notes || e.Evidence != Complete || again.Evidence != Complete {
		t.Fatalf("notes = %v, evidence = %s then %s", e.Notes, e.Evidence, again.Evidence)
	}
}

func TestPartialAndMissingRuns(t *testing.T) {
	f := newFixture(t)
	f.remove(filepath.Join(RunsDir, runID(1), AccountFile))
	f.remove(filepath.Join(RunsDir, runID(2)))
	f.sign()
	e := f.only()
	if e.Evidence != Partial || len(e.Problems) != 0 {
		t.Fatalf("evidence = %s, problems = %v", e.Evidence, e.Problems)
	}
	if run := runByID(t, e, runID(1)); run.Evidence != Partial || run.Files[2].State != Absent {
		t.Fatalf("run 1 = %+v", run)
	}
	if run := runByID(t, e, runID(2)); run.Evidence != Missing || run.Record != nil {
		t.Fatalf("run 2 = %+v", run)
	}
	// A missing journal is reported as missing, never read as empty.
	_, err := f.repo().OpenArtifact(e.ID, runID(1), AccountFile)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestListedButMissingFileIsNamed(t *testing.T) {
	f := newFixture(t)
	f.remove(filepath.Join(RunsDir, runID(1), EventsFile))
	run := runByID(t, f.only(), runID(1))
	if run.Evidence != Partial || run.Files[1].State != Absent || !strings.Contains(run.Files[1].Detail, ChecksumFile) {
		t.Fatalf("run = %+v", run)
	}
}

func TestFailedRunKeepsItsStatus(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(RunsDir, runID(3), RunFile), fmt.Sprintf(`{"run_id":%q,"status":"failed","error":"strategy disconnected"}`, runID(3)))
	f.sign()
	run := runByID(t, f.only(), runID(3))
	// The files are complete; Trader's status says how the run ended.
	if run.Evidence != Complete || run.Record.Status != "failed" || run.Record.Error == "" {
		t.Fatalf("run = %+v", run)
	}
}

func TestUnfinishedRunIsNotEvidence(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(RunsDir, runID(1), RunFile), fmt.Sprintf(`{"run_id":%q,"status":"running"}`, runID(1)))
	f.sign()
	e := f.only()
	if run := runByID(t, e, runID(1)); run.Evidence != Untrusted || run.Record != nil || e.Evidence != Untrusted {
		t.Fatalf("experiment %s, run = %+v", e.Evidence, run)
	}
}

func TestHiddenStagingDirectoriesAreNotListed(t *testing.T) {
	f := newFixture(t)
	staging := filepath.Join(filepath.Dir(f.dir), ".staging-run2")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(f.dir, ManifestFile))
	if err := os.WriteFile(filepath.Join(staging, ManifestFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if e := f.only(); e.Path != pkgPath {
		t.Fatalf("listed %s", e.Path)
	}
}

func TestChecksumMismatch(t *testing.T) {
	f := newFixture(t)
	f.write(filepath.Join(RunsDir, runID(2), EventsFile), `{"seq":1,"state":"rejected"}`+"\n")
	e := f.only()
	if run := runByID(t, e, runID(2)); run.Evidence != Untrusted || run.Files[1].State != Mismatch || e.Evidence != Untrusted {
		t.Fatalf("experiment %s, run = %+v", e.Evidence, run)
	}
	if _, err := f.repo().OpenArtifact(e.ID, runID(2), EventsFile); err == nil {
		t.Fatal("opened a file whose checksum does not match")
	}
}

func TestUnlistedFileIsNotTrusted(t *testing.T) {
	f := newFixture(t)
	f.remove(filepath.Join(RunsDir, runID(1), AccountFile))
	f.sign()
	f.write(filepath.Join(RunsDir, runID(1), AccountFile), "{}\n") // appeared after signing
	run := runByID(t, f.only(), runID(1))
	if run.Evidence != Untrusted || run.Files[2].State != Unlisted {
		t.Fatalf("run = %+v", run)
	}
}

func TestTamperedManifests(t *testing.T) {
	for _, tt := range []struct {
		name    string
		tamper  func(*fixture)
		problem string
	}{
		{"edited manifest", func(f *fixture) {
			data, _ := os.ReadFile(filepath.Join(f.dir, ManifestFile))
			f.write(ManifestFile, strings.Replace(string(data), `"complete"`, `"no_eligible_selection"`, 1))
		}, ManifestFile + " is mismatch"},
		{"manifest not listed", func(f *fixture) {
			data, _ := os.ReadFile(filepath.Join(f.dir, ChecksumFile))
			var keep []string
			for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if !strings.HasSuffix(l, "  "+ManifestFile) {
					keep = append(keep, l)
				}
			}
			f.write(ChecksumFile, strings.Join(keep, "\n")+"\n")
		}, ManifestFile + " is unlisted"},
		{"checksum path escapes", func(f *fixture) {
			data, _ := os.ReadFile(filepath.Join(f.dir, ChecksumFile))
			f.write(ChecksumFile, string(data)+strings.Repeat("0", 64)+"  ../wfa08/run1/experiment.json\n")
		}, "outside the package"},
		{"malformed checksum line", func(f *fixture) {
			data, _ := os.ReadFile(filepath.Join(f.dir, ChecksumFile))
			f.write(ChecksumFile, string(data)+"not a checksum\n")
		}, "malformed"},
		{"checksums missing", func(f *fixture) { f.remove(ChecksumFile) }, ChecksumFile + " is missing"},
		{"invalid RunID in manifest", func(f *fixture) {
			data, _ := os.ReadFile(filepath.Join(f.dir, ManifestFile))
			f.write(ManifestFile, strings.Replace(string(data), runID(1), "../../../etc", -1))
			f.sign()
		}, "invalid RunID"},
		{"run not in manifest", func(f *fixture) {
			f.write(filepath.Join(RunsDir, runID(9), RunFile), `{}`)
			f.sign()
		}, "is not referenced"},
		{"other listed file changed", func(f *fixture) {
			f.write("trader-config.yml", "{}\n")
			f.sign()
			f.write("trader-config.yml", "{risk: 1}\n")
		}, "trader-config.yml is mismatch"},
		{"manifest not JSON", func(f *fixture) {
			f.write(ManifestFile, "{")
			f.sign()
		}, ManifestFile + ":"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.tamper(f)
			e := f.only()
			if e.Evidence != Untrusted || !strings.Contains(strings.Join(e.Problems, "; "), tt.problem) {
				t.Fatalf("evidence = %s, problems = %v, want %q", e.Evidence, e.Problems, tt.problem)
			}
		})
	}
}

func TestManifestMismatchRefusesArtifacts(t *testing.T) {
	f := newFixture(t)
	data, _ := os.ReadFile(filepath.Join(f.dir, ManifestFile))
	f.write(ManifestFile, string(data)+" ")
	if _, err := f.repo().OpenArtifact("wfa09-emax-eurusd-h1-run1", runID(1), RunFile); err == nil ||
		!strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("err = %v", err)
	}
}

func TestDuplicateExperimentIDs(t *testing.T) {
	f := newFixture(t)
	if err := os.CopyFS(filepath.Join(filepath.Dir(f.dir), "run2"), os.DirFS(f.dir)); err != nil {
		t.Fatal(err)
	}
	all, err := f.repo().List()
	if err != nil || len(all) != 2 || all[0].Evidence != Untrusted || all[1].Evidence != Untrusted {
		t.Fatalf("list = %+v, err = %v", all, err)
	}
	if _, err := f.repo().Experiment(all[0].ID); err == nil || !strings.Contains(err.Error(), "2 packages") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenArtifactRejectsUnsafeNames(t *testing.T) {
	f := newFixture(t)
	r := f.repo()
	for _, tt := range []struct{ run, name, want string }{
		{runID(1), "../" + ManifestFile, "not a run artifact"},
		{runID(1), "/etc/passwd", "not a run artifact"},
		{runID(1), ChecksumFile, "not a run artifact"},
		{"../" + runID(1), RunFile, "has no run"},
		{"run_01M8600S0W2PX33XJXVGXV7799", RunFile, "has no run"},
	} {
		if _, err := r.OpenArtifact("wfa09-emax-eurusd-h1-run1", tt.run, tt.name); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s %s: err = %v, want %q", tt.run, tt.name, err, tt.want)
		}
	}
}

func TestSymlinksAreNotEvidence(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(outside, []byte(`{"seq":1,"state":"filled"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{outside, filepath.Join("..", runID(2), EventsFile)} {
		t.Run(filepath.Base(filepath.Dir(target)), func(t *testing.T) {
			f := newFixture(t)
			link := filepath.Join(f.dir, RunsDir, runID(1), EventsFile)
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			e := f.only()
			if run := runByID(t, e, runID(1)); run.Files[1].State != Invalid || e.Evidence != Untrusted {
				t.Fatalf("run = %+v", run)
			}
			if _, err := f.repo().OpenArtifact(e.ID, runID(1), EventsFile); err == nil {
				t.Fatal("opened a symlink as evidence")
			}
		})
	}
}

func TestSymlinkedPackageOutsideTheRepoIsNotListed(t *testing.T) {
	f := newFixture(t)
	elsewhere := newFixture(t)
	if err := os.Symlink(elsewhere.dir, filepath.Join(filepath.Dir(f.dir), "run2")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if e := f.only(); e.Path != pkgPath {
		t.Fatalf("listed %s", e.Path)
	}
}

func TestHistoricalPackages(t *testing.T) {
	r := openRepo(t, "testdata/portfolio")
	all, err := r.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("list = %+v, err = %v", all, err)
	}
	wfa01, wfa02 := all[0], all[1]
	for _, e := range all {
		if e.Manifest != Verified || e.Evidence != Partial || len(e.Problems) != 0 || len(e.Runs) != 9 ||
			e.Status != wfa.StatusComplete || e.Archived || e.Notes {
			t.Fatalf("%s: manifest %s, evidence %s, problems %v, runs %d", e.Path, e.Manifest, e.Evidence, e.Problems, len(e.Runs))
		}
	}
	const oos = "run_01M8600S0W2PX33XJXVGXV770B"
	for _, run := range wfa01.Runs {
		want := Missing
		if run.RunID == oos {
			want = Partial
		}
		if run.Evidence != want {
			t.Fatalf("WFA-01 %s: evidence %s, want %s", run.RunID, run.Evidence, want)
		}
	}
	run := runByID(t, wfa01, oos)
	if run.Phase != wfa.PhaseTest || run.Record == nil || run.Record.Status != "completed" ||
		run.Files[0].State != Verified || run.Files[1].State != Verified || run.Files[2].State != Absent {
		t.Fatalf("WFA-01 OOS = %+v", run)
	}
	for _, run := range wfa02.Runs {
		if run.Evidence != Missing || run.ReportedStatus != wfa.StatusCompleted {
			t.Fatalf("WFA-02 %s = %+v", run.RunID, run)
		}
	}
	if _, err := r.Reconcile(wfa01.ID, oos); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("reconcile without a journal: err = %v", err)
	}
}

// editReport rewrites experiment.json through edit and re-signs the package.
func (f *fixture) editReport(edit func(*wfa.Report)) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, ManifestFile))
	if err != nil {
		f.t.Fatal(err)
	}
	var rep wfa.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		f.t.Fatal(err)
	}
	edit(&rep)
	if data, err = json.Marshal(rep); err != nil {
		f.t.Fatal(err)
	}
	f.write(ManifestFile, string(data))
	f.sign()
}

func TestRunAttemptsWithoutARunIDKeepTheExperimentIncomplete(t *testing.T) {
	for _, tt := range []struct {
		name   string
		run    int
		edit   func(*wfa.Report)
		detail string
	}{
		{"nil outcome", 1, func(r *wfa.Report) { r.Training[0].Outcome, r.Training[0].Err = nil, "strategy did not start" }, "strategy did not start"},
		{"empty RunID", 3, func(r *wfa.Report) {
			r.OOS.Outcome = &wfa.Outcome{Status: "failed", Error: "connection refused"}
		}, "connection refused"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.remove(filepath.Join(RunsDir, runID(tt.run)))
			f.editReport(tt.edit)
			e := f.only()
			// The manifest still claims complete; the evidence cannot.
			if e.Status != wfa.StatusComplete || e.Evidence != Partial || len(e.Runs) != 3 || len(e.Problems) != 0 {
				t.Fatalf("status %s, evidence %s, runs %d, problems %v", e.Status, e.Evidence, len(e.Runs), e.Problems)
			}
			var unassigned []Run
			for _, run := range e.Runs {
				if run.RunID == "" {
					unassigned = append(unassigned, run)
				}
			}
			if len(unassigned) != 1 || unassigned[0].Evidence != Missing || !strings.Contains(unassigned[0].Detail, tt.detail) {
				t.Fatalf("unassigned = %+v", unassigned)
			}
			if _, err := f.repo().OpenArtifact(e.ID, "", RunFile); err == nil {
				t.Fatal("opened a run with no RunID")
			}
		})
	}
}

func TestLeftoverDirectoryOfAnUnassignedAttemptIsAProblem(t *testing.T) {
	f := newFixture(t)
	f.editReport(func(r *wfa.Report) { r.Training[0].Outcome = nil })
	if e := f.only(); e.Evidence != Untrusted || !strings.Contains(strings.Join(e.Problems, "; "), "is not referenced") {
		t.Fatalf("evidence %s, problems %v", e.Evidence, e.Problems)
	}
}

func TestReportWithNoRunsIsMissingEvidence(t *testing.T) {
	f := newFixture(t)
	f.remove(RunsDir)
	f.editReport(func(r *wfa.Report) { r.Training, r.OOS = nil, nil })
	if e := f.only(); e.Status != wfa.StatusComplete || e.Evidence != Missing || len(e.Runs) != 0 || len(e.Problems) != 0 {
		t.Fatalf("status %s, evidence %s, runs %d, problems %v", e.Status, e.Evidence, len(e.Runs), e.Problems)
	}
}

func TestOnlyTerminalTraderStatusesAreEvidence(t *testing.T) {
	for status, want := range map[string]Evidence{
		"completed": Complete, "failed": Complete, "cancelled": Complete,
		"running": Untrusted, "queued": Untrusted, "": Untrusted, "Completed": Untrusted,
	} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			f.write(filepath.Join(RunsDir, runID(1), RunFile), fmt.Sprintf(`{"run_id":%q,"status":%q}`, runID(1), status))
			f.sign()
			e := f.only()
			if run := runByID(t, e, runID(1)); run.Evidence != want || e.Evidence != want {
				t.Fatalf("run %s, experiment %s, want %s: %+v", run.Evidence, e.Evidence, want, run.Files[0])
			}
		})
	}
}
