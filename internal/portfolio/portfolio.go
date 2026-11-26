// Package portfolio reads the Portfolio repository: the archived experiment
// packages under backtests/, the Trader runs each manifest references, and the
// checksums that certify them. It only reads. Every file is opened through an
// os.Root, so no path can leave the repository, and evidence must be a regular
// file, not a symlink. See docs/portfolio.md.
package portfolio

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/investor/internal/wfa"
)

// DefaultRoot is where the Portfolio checkout is when nothing else is configured.
const DefaultRoot = "/srv/portfolio"

// RootEnv names the environment variable that overrides DefaultRoot.
const RootEnv = "INVESTOR_PORTFOLIO_ROOT"

// Names in an experiment package.
const (
	BacktestsDir = "backtests"        // where experiment packages live
	ManifestFile = "experiment.json"  // Investor's report, byte for byte
	ChecksumFile = "checksums.sha256" // sha256sum output for the original files
	ArchiveFile  = "archive.json"     // archive provenance (written by archive)
	NotesFile    = "notes.org"        // free-form research notes, not checksummed
	RunsDir      = "runs"             // runs/<RunID>/ holds Trader's files

	RunFile     = "run.json"      // Trader's record of the run
	EventsFile  = "events.jsonl"  // Trader's order transitions
	AccountFile = "account.jsonl" // Trader's account observations
)

// Artifacts are Trader's files for one run, the only names OpenArtifact opens.
var Artifacts = []string{RunFile, EventsFile, AccountFile}

// FileState is what checking one file against checksums.sha256 found.
type FileState string

const (
	Verified FileState = "verified" // listed, present, and the SHA-256 matches
	Absent   FileState = "missing"  // not present
	Unlisted FileState = "unlisted" // present but not in checksums.sha256
	Mismatch FileState = "mismatch" // present, listed, and the SHA-256 differs
	Invalid  FileState = "invalid"  // not a regular file, or unreadable
)

// Evidence is how complete a run's or an experiment's evidence is.
type Evidence string

const (
	Complete  Evidence = "complete" // every expected file is present and verified
	Partial   Evidence = "partial"  // some expected files are missing; nothing is wrong
	Missing   Evidence = "missing"  // none of a run's files are present, or there is no run
	Untrusted Evidence = "invalid"  // something fails verification; do not rely on it
)

// Terminal is the set of Trader run statuses that end a run. Any other status
// in run.json, including "running" or none, means the run is not evidence.
var Terminal = map[string]bool{"completed": true, "failed": true, "cancelled": true}

var runIDPattern = regexp.MustCompile(`^run_[0-9A-HJKMNP-TV-Z]{26}$`)

// ValidRunID reports whether s is a Trader RunID: "run_" and a 26-character ULID.
func ValidRunID(s string) bool { return runIDPattern.MatchString(s) }

// File is one checked file.
type File struct {
	Name   string    `json:"name"`
	State  FileState `json:"state"`
	Detail string    `json:"detail,omitempty"`
}

// Record is the part of Trader's run.json the catalog reads. Result is Trader's
// summary exactly as recorded; the catalog never recalculates it.
type Record struct {
	RunID     string          `json:"run_id"`
	Kind      string          `json:"kind"`
	Status    string          `json:"status"`
	Error     string          `json:"error,omitempty"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   time.Time       `json:"ended_at,omitzero"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// Run is one Trader run the manifest references, with the state of its files.
// The three statuses are separate: ReportedStatus is what the strategy process
// printed when Investor ran it (from experiment.json), Record.Status is Trader's
// own (from the archived run.json), and Evidence is how much of the run's files
// are archived and verified. A run attempt that ended before Trader assigned a
// RunID has an empty RunID, no files, Evidence Missing and a Detail saying why.
type Run struct {
	RunID          string       `json:"run_id"`
	Detail         string       `json:"detail,omitempty"`
	Phase          wfa.Phase    `json:"phase"`
	Params         wfa.Params   `json:"params"`
	ReportedStatus string       `json:"reported_status"`
	Reported       *wfa.Summary `json:"reported_summary,omitempty"`
	Record         *Record      `json:"record,omitempty"`
	Evidence       Evidence     `json:"evidence"`
	Files          []File       `json:"files"`
}

// Experiment is one package: a directory under backtests/ holding experiment.json.
type Experiment struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"` // relative to the repository root
	Status     string    `json:"status"`
	Strategy   string    `json:"strategy"` // the package path's first segment under backtests/
	Instrument string    `json:"instrument"`
	Interval   string    `json:"interval"`
	Manifest   FileState `json:"manifest"`
	Evidence   Evidence  `json:"evidence"`
	Runs       []Run     `json:"runs"`
	Notes      bool      `json:"notes"`          // notes.org is present
	Archived   bool      `json:"archive_record"` // archive.json is present
	Problems   []string  `json:"problems,omitempty"`

	report wfa.Report
	sums   map[string]string
}

// Repo is an open Portfolio checkout or worktree.
type Repo struct{ root *os.Root }

// Open opens the Portfolio repository at dir.
func Open(dir string) (*Repo, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("portfolio: %w", err)
	}
	return &Repo{root: r}, nil
}

// Close releases the repository.
func (r *Repo) Close() error { return r.root.Close() }

// List finds and checks every experiment package, sorted by path. It verifies
// every checksum, so it reads every archived file. Hidden directories, such as
// staging directories, are skipped. A package that fails a check is listed with
// its problems, not dropped; List fails only when the repository can't be read.
func (r *Repo) List() ([]Experiment, error) {
	var out []Experiment
	err := fs.WalkDir(r.root.FS(), BacktestsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		if _, err := r.root.Lstat(path.Join(p, ManifestFile)); err != nil {
			return nil
		}
		out = append(out, r.load(p))
		return fs.SkipDir // packages do not nest
	})
	if err != nil {
		return nil, fmt.Errorf("portfolio: %w", err)
	}
	ids := map[string][]int{}
	for i, e := range out {
		if e.ID != "" {
			ids[e.ID] = append(ids[e.ID], i)
		}
	}
	for id, at := range ids {
		if len(at) > 1 {
			for _, i := range at {
				out[i].problem(fmt.Sprintf("experiment id %q is used by %d packages", id, len(at)))
			}
		}
	}
	return out, nil
}

// Experiment returns the one package whose manifest id is id.
func (r *Repo) Experiment(id string) (Experiment, error) {
	all, err := r.List()
	if err != nil {
		return Experiment{}, err
	}
	var found []Experiment
	for _, e := range all {
		if e.ID == id {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return Experiment{}, fmt.Errorf("portfolio: no experiment %q", id)
	case 1:
		return found[0], nil
	}
	return Experiment{}, fmt.Errorf("portfolio: experiment id %q is used by %d packages", id, len(found))
}

// Run returns run runID of experiment id.
func (r *Repo) Run(id, runID string) (Experiment, Run, error) {
	e, err := r.Experiment(id)
	if err != nil {
		return e, Run{}, err
	}
	run, err := e.run(runID)
	return e, run, err
}

// OpenArtifact opens one of Trader's files for run runID of experiment id, read
// only. The file is hashed through the handle it returns, which is then rewound,
// so the bytes read are the bytes verified; an in-place write after that is not
// detected. It refuses a file whose checksum is missing or does not match, and
// any run of a package whose manifest does not verify.
func (r *Repo) OpenArtifact(id, runID, name string) (io.ReadCloser, error) {
	if !slices.Contains(Artifacts, name) {
		return nil, fmt.Errorf("portfolio: %q is not a run artifact (want one of %s)", name, strings.Join(Artifacts, ", "))
	}
	e, run, err := r.Run(id, runID)
	if err != nil {
		return nil, err
	}
	if e.Manifest != Verified {
		return nil, fmt.Errorf("portfolio: %s: %s is %s; its runs are not trusted", id, ManifestFile, e.Manifest)
	}
	rel := path.Join(RunsDir, runID, name)
	for _, f := range run.Files {
		if f.Name == name && f.State != Verified {
			return nil, fmt.Errorf("portfolio: %s: %s is %s%s", id, rel, f.State, suffix(f.Detail))
		}
	}
	want, ok := e.sums[rel]
	if !ok {
		return nil, fmt.Errorf("portfolio: %s: %s is not in %s", id, rel, ChecksumFile)
	}
	f, err := r.openRegular(path.Join(e.Path, rel))
	if err != nil {
		return nil, fmt.Errorf("portfolio: %s: %w", id, err)
	}
	got, err := hashOf(f)
	if err == nil && got != want {
		err = fmt.Errorf("%s: SHA-256 mismatch", rel)
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("portfolio: %s: %w", id, err)
	}
	return f, nil
}

// Reconcile validates the archived account journal of run runID against the
// summary experiment.json recorded for it and returns the equity curve.
func (r *Repo) Reconcile(id, runID string) ([]wfa.EquityPoint, error) {
	e, err := r.Experiment(id)
	if err != nil {
		return nil, err
	}
	f, err := r.OpenArtifact(id, runID, AccountFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return e.report.Reconcile(runID, f)
}

func (e Experiment) run(runID string) (Run, error) {
	for _, run := range e.Runs {
		if run.RunID == runID && ValidRunID(runID) {
			return run, nil
		}
	}
	return Run{}, fmt.Errorf("portfolio: experiment %q has no run %q", e.ID, runID)
}

func (e *Experiment) problem(s string) {
	e.Problems = append(e.Problems, s)
	e.Evidence = Untrusted
}

// load checks the package at dir. It never fails: what it cannot read becomes a
// problem, and any problem makes the evidence untrusted.
func (r *Repo) load(dir string) Experiment {
	e := Experiment{Path: dir}
	if seg := strings.Split(dir, "/"); len(seg) > 1 {
		e.Strategy = seg[1]
	}
	if sums, err := r.checksums(dir); err != nil {
		e.Problems = append(e.Problems, err.Error())
	} else {
		e.sums = sums
	}

	data, state, detail := r.readChecked(dir, ManifestFile, e.sums)
	e.Manifest = state
	if state != Verified {
		e.Problems = append(e.Problems, fmt.Sprintf("%s is %s%s", ManifestFile, state, suffix(detail)))
	}
	if data == nil {
		e.Evidence = Untrusted
		return e
	}
	if err := json.Unmarshal(data, &e.report); err != nil {
		e.problem(fmt.Sprintf("%s: %v", ManifestFile, err))
		return e
	}
	e.ID, e.Status = e.report.ID, e.report.Status
	e.Instrument, e.Interval = e.report.Spec.Instrument, e.report.Spec.Interval
	if e.ID == "" {
		e.Problems = append(e.Problems, ManifestFile+" has no id")
	}

	checked := map[string]bool{ManifestFile: true}
	seen := map[string]bool{}
	for _, mr := range e.report.Runs() {
		if mr.Outcome == nil || mr.Outcome.RunID == "" {
			// The attempt ended before Trader assigned a RunID: it stays in the
			// count, so the experiment's evidence can never read complete.
			run := Run{Phase: mr.Phase, Params: mr.Params, Evidence: Missing, Detail: "no RunID: " + orNone(mr.Err)}
			if mr.Outcome != nil {
				run.ReportedStatus, run.Reported = mr.Outcome.Status, mr.Outcome.Summary
				if mr.Err == "" && mr.Outcome.Error != "" {
					run.Detail = "no RunID: " + mr.Outcome.Error
				}
			}
			e.Runs = append(e.Runs, run)
			continue
		}
		id := mr.Outcome.RunID
		switch {
		case !ValidRunID(id):
			e.Problems = append(e.Problems, fmt.Sprintf("%s names an invalid RunID %q", ManifestFile, id))
			continue
		case seen[id]:
			e.Problems = append(e.Problems, fmt.Sprintf("%s names RunID %s twice", ManifestFile, id))
			continue
		}
		seen[id] = true
		run := Run{RunID: id, Phase: mr.Phase, Params: mr.Params, ReportedStatus: mr.Outcome.Status, Reported: mr.Outcome.Summary}
		e.Runs = append(e.Runs, r.checkRun(&e, run, checked))
	}

	if entries, err := fs.ReadDir(r.root.FS(), path.Join(dir, RunsDir)); err == nil {
		for _, d := range entries {
			if !seen[d.Name()] {
				e.Problems = append(e.Problems, fmt.Sprintf("%s/%s is not referenced by %s", RunsDir, d.Name(), ManifestFile))
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		e.Problems = append(e.Problems, fmt.Sprintf("%s: %v", RunsDir, err))
	}

	// Every other listed file must still match, such as a recorded Trader config.
	for _, name := range sortedKeys(e.sums) {
		if checked[name] {
			continue
		}
		if state, detail := r.check(path.Join(dir, name), e.sums[name], true); state != Verified {
			e.Problems = append(e.Problems, fmt.Sprintf("%s is %s%s", name, state, suffix(detail)))
		}
	}

	e.Notes = r.isRegular(path.Join(dir, NotesFile))
	e.Archived = r.isRegular(path.Join(dir, ArchiveFile))
	e.Evidence = e.overall()
	return e
}

// checkRun checks one run's files and reads its verified run.json.
func (r *Repo) checkRun(e *Experiment, run Run, checked map[string]bool) Run {
	counts := map[FileState]int{}
	for _, name := range Artifacts {
		rel := path.Join(RunsDir, run.RunID, name)
		checked[rel] = true
		want, listed := e.sums[rel]
		var data []byte
		state, detail := Absent, ""
		if name == RunFile {
			data, state, detail = r.readChecked(e.Path, rel, e.sums)
		} else {
			state, detail = r.check(path.Join(e.Path, rel), want, listed)
		}
		if state == Absent && listed {
			detail = "listed in " + ChecksumFile
		}
		if state == Verified && name == RunFile {
			var rec Record
			switch err := json.Unmarshal(data, &rec); {
			case err != nil:
				state, detail = Invalid, err.Error()
			case rec.RunID != run.RunID:
				state, detail = Invalid, fmt.Sprintf("run_id is %q", rec.RunID)
			case !Terminal[rec.Status]:
				state, detail = Invalid, fmt.Sprintf("status %q is not a terminal Trader status", rec.Status)
			default:
				run.Record = &rec
			}
		}
		counts[state]++
		run.Files = append(run.Files, File{Name: name, State: state, Detail: detail})
	}
	switch {
	case counts[Verified] == len(Artifacts):
		run.Evidence = Complete
	case counts[Absent] == len(Artifacts):
		run.Evidence = Missing
	case counts[Verified]+counts[Absent] == len(Artifacts):
		run.Evidence = Partial
	default:
		run.Evidence = Untrusted
	}
	return run
}

func (e Experiment) overall() Evidence {
	if len(e.Problems) > 0 || e.Manifest != Verified {
		return Untrusted
	}
	if len(e.Runs) == 0 {
		return Missing // a report with no runs has no evidence to be complete
	}
	for _, run := range e.Runs {
		if run.Evidence == Untrusted {
			return Untrusted
		}
	}
	for _, run := range e.Runs {
		if run.Evidence != Complete {
			return Partial
		}
	}
	return Complete
}

// readChecked reads dir/name whole and checks it against sums. The bytes are
// returned whenever the file could be read, whatever its state.
func (r *Repo) readChecked(dir, name string, sums map[string]string) ([]byte, FileState, string) {
	f, err := r.openRegular(path.Join(dir, name))
	if err != nil {
		return nil, stateOf(err), detailOf(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, Invalid, err.Error()
	}
	want, listed := sums[name]
	switch sum := sha256.Sum256(data); {
	case !listed:
		return data, Unlisted, ""
	case hex.EncodeToString(sum[:]) != want:
		return data, Mismatch, ""
	}
	return data, Verified, ""
}

// check hashes the file at name and compares it with want.
func (r *Repo) check(name, want string, listed bool) (FileState, string) {
	f, err := r.openRegular(name)
	if err != nil {
		return stateOf(err), detailOf(err)
	}
	defer f.Close()
	got, err := hashOf(f)
	switch {
	case err != nil:
		return Invalid, err.Error()
	case !listed:
		return Unlisted, ""
	case got != want:
		return Mismatch, ""
	}
	return Verified, ""
}

var errNotRegular = errors.New("not a regular file")

// openRegular opens name read-only, refusing anything but a regular file. The
// os.Root refuses paths and symlinks that leave the repository.
func (r *Repo) openRegular(name string) (*os.File, error) {
	fi, err := r.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	f, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: %w", name, errNotRegular)
	}
	return f, nil
}

func (r *Repo) isRegular(name string) bool {
	fi, err := r.root.Lstat(name)
	return err == nil && fi.Mode().IsRegular()
}

func stateOf(err error) FileState {
	if errors.Is(err, fs.ErrNotExist) {
		return Absent
	}
	return Invalid
}

func detailOf(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return err.Error()
}

func hashOf(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksums reads dir/checksums.sha256, in sha256sum's format. Any malformed,
// duplicate or non-local entry rejects the whole file: a checksum file that
// can't be fully trusted verifies nothing.
func (r *Repo) checksums(dir string) (map[string]string, error) {
	f, err := r.openRegular(path.Join(dir, ChecksumFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s is missing", ChecksumFile)
		}
		return nil, fmt.Errorf("%s: %v", ChecksumFile, err)
	}
	defer f.Close()
	sums := map[string]string{}
	scan := bufio.NewScanner(f)
	for n := 1; scan.Scan(); n++ {
		line := scan.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		sum, name, ok := parseSumLine(string(line))
		if !ok {
			return nil, fmt.Errorf("%s line %d is malformed", ChecksumFile, n)
		}
		if !localPath(name) {
			return nil, fmt.Errorf("%s line %d names a path outside the package: %q", ChecksumFile, n, name)
		}
		if _, dup := sums[name]; dup {
			return nil, fmt.Errorf("%s lists %s twice", ChecksumFile, name)
		}
		sums[name] = sum
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("%s: %v", ChecksumFile, err)
	}
	return sums, nil
}

var hexSum = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parseSumLine reads "<sha256>  <name>" or "<sha256> *<name>".
func parseSumLine(line string) (sum, name string, ok bool) {
	if len(line) < 66 || !hexSum.MatchString(line[:64]) || line[64] != ' ' || (line[65] != ' ' && line[65] != '*') {
		return "", "", false
	}
	return line[:64], line[66:], line[66:] != ""
}

// localPath reports whether name is a clean, relative, slash-separated path
// that stays inside its directory.
func localPath(name string) bool {
	return name == path.Clean(name) && !path.IsAbs(name) && name != "." &&
		name != ".." && !strings.HasPrefix(name, "../") && !strings.Contains(name, `\`)
}

func suffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func orNone(s string) string {
	if s == "" {
		return "no error recorded"
	}
	return s
}
