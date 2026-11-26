package portfolio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	newPath = "backtests/emax/EURUSD/H1/wfa09/run1"
	newID   = "wfa09-emax-eurusd-h1-run1"
)

var archivedAt = time.Date(2026, 11, 26, 20, 0, 0, 0, time.UTC)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.test",
		"-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// workspace is a temporary Portfolio repository with a worktree for one
// experiment, an ingress holding that experiment's runs, and its report.
type workspace struct {
	t            *testing.T
	main, tree   string
	report       []byte
	journalBytes []byte
}

// newWorkspace commits files (relative path to content) to main, makes the
// experiment's worktree and writes its three finished runs to the ingress.
func newWorkspace(t *testing.T, files map[string][]byte) *workspace {
	t.Helper()
	base := t.TempDir()
	w := &workspace{t: t, main: filepath.Join(base, "portfolio"), tree: filepath.Join(base, "tree")}
	// The report and run files of the reader's complete fixture serve as the source.
	f := newFixture(t)
	read := func(rel string) []byte {
		data, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	w.report = read(ManifestFile)
	for i := 1; i <= 3; i++ {
		for _, name := range Artifacts {
			w.put(filepath.Join(w.main, IngressDir, runID(i), name), read(RunsDir+"/"+runID(i)+"/"+name))
		}
	}
	w.put(filepath.Join(w.main, ".gitignore"), []byte(".incoming/\n.staging-*\n"))
	w.put(filepath.Join(w.main, BacktestsDir, "README.md"), []byte("# Backtests\n"))
	for rel, data := range files {
		w.put(filepath.Join(w.main, filepath.FromSlash(rel)), data)
	}
	git(t, w.main, "init", "-q", "-b", "main")
	git(t, w.main, "add", "-A")
	git(t, w.main, "commit", "-qm", "init")
	git(t, w.main, "worktree", "add", "-q", "-b", Branch(newID), w.tree)
	return w
}

func (w *workspace) put(path string, data []byte) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) ingress(run int, name string) string {
	return filepath.Join(w.main, IngressDir, runID(run), name)
}

func (w *workspace) request() ArchiveRequest {
	return ArchiveRequest{Portfolio: w.main, Worktree: w.tree, Report: w.report, Path: newPath, Now: archivedAt}
}

// snapshot is every file below dir, .git excluded, with its content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), stagingPrefix) {
				t.Errorf("staging directory left behind: %s", rel)
			}
			return nil
		}
		data, err := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestArchiveCopiesFinishedRunsByteForByte(t *testing.T) {
	w := newWorkspace(t, nil)
	mainBefore := snapshot(t, w.main)

	e, rec, err := Archive(w.request())
	if err != nil {
		t.Fatal(err)
	}
	if e.Evidence != Complete || e.Path != newPath || !e.Archived || e.Notes || len(e.Problems) != 0 || e.CompleteRunCount() != 3 {
		t.Fatalf("experiment = %+v", e)
	}
	got := snapshot(t, filepath.Join(w.tree, newPath))
	if got[ManifestFile] != string(w.report) {
		t.Fatal("experiment.json differs from the report")
	}
	for i := 1; i <= 3; i++ {
		for _, name := range Artifacts {
			want, _ := os.ReadFile(w.ingress(i, name))
			if got[RunsDir+"/"+runID(i)+"/"+name] != string(want) {
				t.Fatalf("run %d %s differs from the ingress", i, name)
			}
		}
	}
	var stored ArchiveRecord
	if err := json.Unmarshal([]byte(got[ArchiveFile]), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 1 || stored.ExperimentID != newID || stored.PackagePath != newPath || stored.CompletesExisting ||
		!stored.ArchivedAt.Equal(archivedAt) || stored.Source != filepath.Join(w.main, IngressDir) || len(stored.Runs) != 3 {
		t.Fatalf("archive.json = %+v", stored)
	}
	first := stored.Runs[0]
	if first.TraderStatus != "completed" || len(first.Files) != 3 || len(first.Missing) != 0 || first.Files[0].Origin != OriginCopied ||
		first.Files[0].SHA256 != sumOf([]byte(got[RunsDir+"/"+runID(1)+"/"+RunFile])) || first.Files[0].Bytes == 0 {
		t.Fatalf("run record = %+v", first)
	}
	if len(rec.Runs) != 3 {
		t.Fatalf("returned record = %+v", rec)
	}
	check := exec.Command("sha256sum", "--check", "--strict", "--quiet", ChecksumFile)
	check.Dir = filepath.Join(w.tree, newPath)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("sha256sum -c: %v: %s", err, out)
	}
	if n := strings.Count(got[ChecksumFile], "\n"); n != 11 { // 9 run files, the manifest and archive.json
		t.Fatalf("checksum lines = %d:\n%s", n, got[ChecksumFile])
	}
	if after := snapshot(t, w.main); fmt.Sprint(after) != fmt.Sprint(mainBefore) {
		t.Fatal("the main checkout or the ingress changed")
	}
}

func TestArchiveRecordsFailedAndMissingRunsAsSuch(t *testing.T) {
	w := newWorkspace(t, nil)
	w.put(w.ingress(1, RunFile), []byte(fmt.Sprintf(`{"run_id":%q,"status":"failed","error":"boom"}`, runID(1))))
	os.Remove(w.ingress(1, AccountFile))
	os.RemoveAll(filepath.Dir(w.ingress(2, RunFile)))
	w.put(w.ingress(3, RunFile), []byte(fmt.Sprintf(`{"run_id":%q,"status":"cancelled"}`, runID(3))))

	e, rec, err := Archive(w.request())
	if err != nil {
		t.Fatal(err)
	}
	if e.Evidence != Partial || e.CompleteRunCount() != 1 {
		t.Fatalf("evidence = %s, complete = %d", e.Evidence, e.CompleteRunCount())
	}
	got := []string{}
	for _, run := range rec.Runs {
		got = append(got, fmt.Sprintf("%s %d %v", run.TraderStatus, len(run.Files), run.Missing))
	}
	want := []string{"failed 2 [account.jsonl]", " 0 [run.json events.jsonl account.jsonl]", "cancelled 3 []"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("runs = %q, want %q", got, want)
	}
}

func TestArchiveRefusesAndLeavesTheWorktreeAsItWas(t *testing.T) {
	record := func(status string) []byte { return []byte(fmt.Sprintf(`{"run_id":%q,"status":%q}`, runID(2), status)) }
	for name, tc := range map[string]struct {
		arrange func(*workspace, *ArchiveRequest)
		want    string
	}{
		"running":    {func(w *workspace, _ *ArchiveRequest) { w.put(w.ingress(2, RunFile), record("running")) }, "not finalized"},
		"no status":  {func(w *workspace, _ *ArchiveRequest) { w.put(w.ingress(2, RunFile), record("")) }, "not finalized"},
		"no record":  {func(w *workspace, _ *ArchiveRequest) { os.Remove(w.ingress(2, RunFile)) }, "not finalized"},
		"bad record": {func(w *workspace, _ *ArchiveRequest) { w.put(w.ingress(2, RunFile), []byte("{")) }, "not finalized"},
		"another run's record": {func(w *workspace, _ *ArchiveRequest) {
			w.put(w.ingress(2, RunFile), []byte(fmt.Sprintf(`{"run_id":%q,"status":"completed"}`, runID(1))))
		}, "is of run"},
		"symlinked journal": {func(w *workspace, _ *ArchiveRequest) {
			os.Remove(w.ingress(2, AccountFile))
			if err := os.Symlink(w.ingress(1, AccountFile), w.ingress(2, AccountFile)); err != nil {
				w.t.Fatal(err)
			}
		}, "not a regular file"},
		"run is a file": {func(w *workspace, _ *ArchiveRequest) {
			os.RemoveAll(filepath.Dir(w.ingress(2, RunFile)))
			w.put(filepath.Join(w.main, IngressDir, runID(2)), []byte("x"))
		}, "not a directory"},
		"no ingress":    {func(w *workspace, _ *ArchiveRequest) { os.RemoveAll(filepath.Join(w.main, ".incoming")) }, "no new run files"},
		"main checkout": {func(w *workspace, r *ArchiveRequest) { r.Worktree = w.main }, "main checkout"},
		"inside the worktree": {func(w *workspace, r *ArchiveRequest) {
			r.Worktree = filepath.Join(w.tree, "sub")
			w.put(filepath.Join(r.Worktree, "x"), nil)
		}, "not the top"},
		"not a repository":   {func(w *workspace, r *ArchiveRequest) { r.Worktree = w.t.TempDir() }, "git"},
		"another repository": {func(w *workspace, r *ArchiveRequest) { r.Portfolio = newWorkspace(w.t, nil).main }, "not a worktree of"},
		"another branch":     {func(w *workspace, _ *ArchiveRequest) { git(w.t, w.tree, "switch", "-qc", "research/other") }, "archived only on"},
		"detached":           {func(w *workspace, _ *ArchiveRequest) { git(w.t, w.tree, "switch", "-q", "--detach") }, "symbolic-ref"},
		"no path":            {func(_ *workspace, r *ArchiveRequest) { r.Path = "" }, "give its package path"},
		"escaping path":      {func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/EURUSD/H1/../run1" }, "package path"},
		"short path":         {func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/EURUSD/H1/wfa09" }, "package path"},
		"hidden path":        {func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/EURUSD/H1/.wfa/run1" }, "package path"},
		"outside backtests":  {func(_ *workspace, r *ArchiveRequest) { r.Path = "other/emax/EURUSD/H1/wfa09/run1" }, "package path"},
		"wrong instrument":   {func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/GBPUSD/H1/wfa09/run1" }, "does not match"},
		"wrong interval":     {func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/EURUSD/D1/wfa09/run1" }, "does not match"},
		"path exists":        {func(w *workspace, _ *ArchiveRequest) { w.put(filepath.Join(w.tree, newPath, "x"), nil) }, "already exists"},
		"inside a package": {func(w *workspace, _ *ArchiveRequest) {
			w.put(filepath.Join(w.tree, "backtests/emax/EURUSD/H1", ManifestFile), []byte("{}"))
		}, "inside the package"},
		"leftover staging": {func(w *workspace, _ *ArchiveRequest) {
			w.put(filepath.Join(w.tree, "backtests/emax/EURUSD/H1/wfa09", stagingPrefix+newID, "x"), nil)
		}, "interrupted"},
		"bad report": {func(_ *workspace, r *ArchiveRequest) { r.Report = []byte("{") }, "report"},
		"unsafe id": {func(_ *workspace, r *ArchiveRequest) {
			r.Report = bytes.Replace(r.Report, []byte(newID), []byte("../x"), 1)
		}, "cannot name"},
		"invalid RunID": {func(_ *workspace, r *ArchiveRequest) {
			r.Report = bytes.Replace(r.Report, []byte(runID(2)), []byte("../escape"), 1)
		}, "invalid RunID"},
		"repeated RunID": {func(_ *workspace, r *ArchiveRequest) {
			r.Report = bytes.Replace(r.Report, []byte(runID(2)), []byte(runID(1)), 1)
		}, "twice"},
		"report of no run": {func(_ *workspace, r *ArchiveRequest) { r.Report = []byte(`{"id":"` + newID + `"}`) }, "names no Trader run"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t, nil)
			req := w.request()
			tc.arrange(w, &req)
			var before map[string]string
			if name != "leftover staging" {
				before = snapshot(t, w.tree)
			}

			_, _, err := Archive(req)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if before != nil {
				if after := snapshot(t, w.tree); fmt.Sprint(after) != fmt.Sprint(before) {
					t.Fatalf("the worktree changed: %v", after)
				}
			}
		})
	}
}

func TestArchiveStopsWhenASourceChangesWhileCopied(t *testing.T) {
	for name, tc := range map[string]struct {
		file   string
		change func(path string) error
	}{
		"journal grows": {AccountFile, func(p string) error {
			f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.WriteString("{}\n")
			return err
		}},
		"journal replaced": {EventsFile, func(p string) error {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := os.WriteFile(p+".new", data, 0o644); err != nil {
				return err
			}
			return os.Rename(p+".new", p)
		}},
		"record rewritten after it was copied": {AccountFile, func(p string) error {
			rec := filepath.Join(filepath.Dir(p), RunFile)
			return os.WriteFile(rec, []byte(fmt.Sprintf(`{"run_id":%q,"status":"failed"}`, runID(1))), 0o644)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t, nil)
			before := snapshot(t, w.tree)
			done := false
			afterCopy = func(from string) {
				if !done && from == runID(1)+"/"+tc.file {
					done = true
					if err := tc.change(w.ingress(1, tc.file)); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Cleanup(func() { afterCopy = func(string) {} })

			_, _, err := Archive(w.request())

			if !done || err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("changed = %v, err = %v", done, err)
			}
			if after := snapshot(t, w.tree); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatal("the worktree changed")
			}
		})
	}
}

// partialPackage is a merged package like WFA-02's: the manifest and a config,
// no runs, with a README the checksums do not list, and notes.
func partialPackage(report []byte, extra map[string][]byte) map[string][]byte {
	config := []byte("{}\n")
	files := map[string][]byte{ManifestFile: report, "trader-config.yml": config, "README.md": []byte("partial\n"), NotesFile: []byte("* Questions\n")}
	sums := sumOf(report) + "  " + ManifestFile + "\n" + sumOf(config) + "  trader-config.yml\n"
	for name, data := range extra {
		files[name] = data
		sums += sumOf(data) + "  " + name + "\n"
	}
	files[ChecksumFile] = []byte(sums)
	out := map[string][]byte{}
	for name, data := range files {
		out[newPath+"/"+name] = data
	}
	return out
}

// sourceReport is the report newWorkspace uses, for committing a package first.
func sourceReport(t *testing.T) (report []byte, run1 []byte) {
	t.Helper()
	w := newWorkspace(t, nil)
	run1, err := os.ReadFile(w.ingress(1, RunFile))
	if err != nil {
		t.Fatal(err)
	}
	return w.report, run1
}

func TestArchiveCompletesAPartialPackageWithoutChangingIt(t *testing.T) {
	report, run1 := sourceReport(t)
	existing := RunsDir + "/" + runID(1) + "/" + RunFile
	w := newWorkspace(t, partialPackage(report, map[string][]byte{existing: run1}))
	pkg := filepath.Join(w.tree, newPath)
	before := snapshot(t, pkg)
	req := w.request()
	req.Path = "" // found by the report's id

	e, rec, err := Archive(req)
	if err != nil {
		t.Fatal(err)
	}
	if e.Evidence != Complete || e.Path != newPath || !e.Archived || !e.Notes || !rec.CompletesExisting {
		t.Fatalf("experiment = %+v, record = %+v", e, rec)
	}
	after := snapshot(t, pkg)
	for name, content := range before {
		if name != ChecksumFile && after[name] != content {
			t.Fatalf("%s changed", name)
		}
	}
	if !strings.HasPrefix(after[ChecksumFile], before[ChecksumFile]) {
		t.Fatalf("checksum lines were not kept:\n%s", after[ChecksumFile])
	}
	if strings.Contains(after[ChecksumFile], "README.md") || strings.Contains(after[ChecksumFile], NotesFile) ||
		strings.Count(after[ChecksumFile], "\n") != strings.Count(before[ChecksumFile], "\n")+9 { // 8 run files and archive.json
		t.Fatalf("checksums:\n%s", after[ChecksumFile])
	}
	if f := rec.Runs[0].Files; f[0].Origin != OriginArchived || f[1].Origin != OriginCopied || rec.Runs[0].TraderStatus != "completed" {
		t.Fatalf("run 1 = %+v", rec.Runs[0])
	}

	// Once archived, the package is closed.
	if _, _, err := Archive(req); err == nil || !strings.Contains(err.Error(), "never changed") {
		t.Fatalf("second archive: %v", err)
	}
}

func TestArchiveRefusesToCompleteWhatItShouldNot(t *testing.T) {
	report, run1 := sourceReport(t)
	existing := RunsDir + "/" + runID(1) + "/" + RunFile
	for name, tc := range map[string]struct {
		files   map[string][]byte
		arrange func(*workspace, *ArchiveRequest)
		want    string
	}{
		"another report": {partialPackage(report, nil), func(_ *workspace, r *ArchiveRequest) { r.Report = append(r.Report, '\n') }, "different experiment.json"},
		"another path":   {partialPackage(report, nil), func(_ *workspace, r *ArchiveRequest) { r.Path = "backtests/emax/EURUSD/H1/wfa09/run2" }, "already at"},
		"tampered package": {partialPackage(report, nil), func(w *workspace, _ *ArchiveRequest) {
			w.put(filepath.Join(w.tree, newPath, "trader-config.yml"), []byte("changed"))
		}, "does not verify"},
		"unlisted evidence": {partialPackage(report, nil), func(w *workspace, _ *ArchiveRequest) {
			w.put(filepath.Join(w.tree, newPath, existing), run1)
		}, "does not verify"},
		"has archive.json": {partialPackage(report, map[string][]byte{ArchiveFile: []byte("{}")}), nil, "never changed"},
		"ingress differs from the archived file": {partialPackage(report, map[string][]byte{existing: run1}), func(w *workspace, _ *ArchiveRequest) {
			w.put(w.ingress(1, RunFile), append(bytes.Clone(run1), '\n'))
		}, "not the archived one"},
		"nothing to add": {partialPackage(report, nil), func(w *workspace, _ *ArchiveRequest) { os.RemoveAll(filepath.Join(w.main, ".incoming")) }, "no new run files"},
		"listed file differs": {map[string][]byte{
			newPath + "/" + ManifestFile: report,
			newPath + "/" + ChecksumFile: []byte(sumOf(report) + "  " + ManifestFile + "\n" + sumOf([]byte("other")) + "  " + existing + "\n"),
		}, nil, "another checksum"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorkspace(t, tc.files)
			req := w.request()
			req.Path = ""
			if tc.arrange != nil {
				tc.arrange(w, &req)
			}
			before := snapshot(t, w.tree)

			_, _, err := Archive(req)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if after := snapshot(t, w.tree); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatal("the worktree changed")
			}
		})
	}
}

func TestArchiveRefusesACompletePackage(t *testing.T) {
	w := newWorkspace(t, nil)
	if _, _, err := Archive(w.request()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(w.tree, newPath, ArchiveFile)); err != nil {
		t.Fatal(err)
	}
	// Re-sign without archive.json: a complete package with no record of an archive.
	r, err := Open(w.tree)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	os.Remove(filepath.Join(w.tree, newPath, ChecksumFile))
	if err := r.writeChecksums(newPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Archive(w.request()); err == nil || !strings.Contains(err.Error(), "already complete") {
		t.Fatalf("err = %v", err)
	}
}

// Completion closes a package, so it must not happen while anything is lacking.
func TestArchiveDoesNotCloseAPackageThatWouldStayIncomplete(t *testing.T) {
	report, _ := sourceReport(t)
	noRunID := bytes.Replace(report, []byte(`"run_id":"`+runID(2)+`"`), []byte(`"run_id":""`), 1)
	for name, tc := range map[string]struct {
		report  []byte
		arrange func(*workspace)
		lacking string
	}{
		"a run is absent": {report, func(w *workspace) { os.RemoveAll(filepath.Dir(w.ingress(2, RunFile))) },
			runID(2) + " run.json,events.jsonl,account.jsonl"},
		"a journal is absent": {report, func(w *workspace) { os.Remove(w.ingress(3, AccountFile)) }, runID(3) + " account.jsonl"},
		"a failed run lacks a journal": {report, func(w *workspace) {
			w.put(w.ingress(1, RunFile), []byte(fmt.Sprintf(`{"run_id":%q,"status":"failed"}`, runID(1))))
			os.Remove(w.ingress(1, EventsFile))
		}, runID(1) + " events.jsonl"},
		"an attempt without a RunID": {noRunID, func(*workspace) {}, "attempt (no RunID"},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "an attempt without a RunID" && bytes.Equal(noRunID, report) {
				t.Fatal("the report was not changed")
			}
			w := newWorkspace(t, partialPackage(tc.report, nil))
			req := w.request()
			req.Report, req.Path = tc.report, ""
			tc.arrange(w)
			before := snapshot(t, w.tree)

			_, _, err := Archive(req)

			if err == nil || !strings.Contains(err.Error(), "left unchanged") || !strings.Contains(err.Error(), tc.lacking) {
				t.Fatalf("err = %v, want it to name %q", err, tc.lacking)
			}
			if after := snapshot(t, w.tree); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("the package changed: %v", after)
			}
		})
	}
}

// The same package completes once what it lacked has arrived.
func TestArchiveCompletesAfterARefusedAttempt(t *testing.T) {
	report, _ := sourceReport(t)
	w := newWorkspace(t, partialPackage(report, nil))
	req := w.request()
	req.Path = ""
	journal, err := os.ReadFile(w.ingress(3, AccountFile))
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(w.ingress(3, AccountFile))
	if _, _, err := Archive(req); err == nil {
		t.Fatal("an incomplete completion was accepted")
	}
	w.put(w.ingress(3, AccountFile), journal)

	e, _, err := Archive(req)

	if err != nil || e.Evidence != Complete {
		t.Fatalf("evidence = %s, err = %v", e.Evidence, err)
	}
}

// A package moved aside by an interrupted completion may be the only copy of
// uncommitted notes, so nothing runs over it.
func TestArchiveStopsAtAPackageLeftAside(t *testing.T) {
	w := newWorkspace(t, nil)
	left := filepath.Join(w.tree, "backtests/emax/EURUSD/H1/wfa09", stagingPrefix+newID+asideSuffix, NotesFile)
	w.put(left, []byte("only copy"))

	_, _, err := Archive(w.request())

	if err == nil || !strings.Contains(err.Error(), "only copy") || !strings.Contains(err.Error(), "Recovery") {
		t.Fatalf("err = %v", err)
	}
	if data, rerr := os.ReadFile(left); rerr != nil || string(data) != "only copy" {
		t.Fatalf("left aside = %q, %v", data, rerr)
	}
}
