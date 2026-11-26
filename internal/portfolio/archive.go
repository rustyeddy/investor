package portfolio

import (
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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rustyeddy/investor/internal/wfa"
)

// IngressDir is where Trader writes its runs, relative to the main Portfolio
// checkout: Trader's results root.
const IngressDir = ".incoming/runs"

// ReportFile is the human summary of a package. Like notes.org it is not
// checksummed.
const ReportFile = "report.md"

// ArchiveSchemaVersion is the version of archive.json that Archive writes.
const ArchiveSchemaVersion = 1

// stagingPrefix starts the name of a directory a package is assembled in. The
// reader skips it, as it does every hidden directory.
const stagingPrefix = ".staging-"

// Where an archived file came from.
const (
	OriginCopied   = "copied"           // copied from the ingress by this archive
	OriginArchived = "already_archived" // in the package before it
)

// ArchiveRecord is archive.json: what one archive step put in a package and where
// it came from. It is evidence, listed in checksums.sha256 and never edited.
type ArchiveRecord struct {
	SchemaVersion int       `json:"schema_version"`
	ExperimentID  string    `json:"experiment_id"`
	PackagePath   string    `json:"package_path"`
	ArchivedAt    time.Time `json:"archived_at"`
	// Source is the ingress directory the copied files were read from.
	Source string `json:"source"`
	// CompletesExisting is true when files were added to a package that was
	// already in the repository.
	CompletesExisting bool          `json:"completes_existing"`
	Runs              []ArchivedRun `json:"runs"`
}

// ArchivedRun is one run the manifest names.
type ArchivedRun struct {
	RunID string `json:"run_id"`
	// TraderStatus is the status in Trader's run.json: completed, failed or
	// cancelled, or empty when there is no run.json.
	TraderStatus string         `json:"trader_status,omitempty"`
	Files        []ArchivedFile `json:"files"`
	Missing      []string       `json:"missing,omitempty"`
}

// ArchivedFile is one of Trader's files as it is in the package.
type ArchivedFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Origin string `json:"origin"`
}

// ArchiveRequest is what to archive and where.
type ArchiveRequest struct {
	// Portfolio is the main Portfolio checkout. It is only read: Trader's runs
	// are in its ingress.
	Portfolio string
	// Worktree is the experiment's linked worktree, the only place written.
	Worktree string
	// Report is Investor's report of the experiment, kept byte for byte as
	// experiment.json.
	Report []byte
	// Path is the new package's path in the repository, such as
	// backtests/emax/EURUSD/H1/wfa03/run1. A package that already has the
	// report's id is completed where it is; Path is then empty or that path.
	Path string
	// Now is recorded as the archive time; the zero value means now.
	Now time.Time
}

// afterCopy is called after a file is copied and before it is checked for
// change. Tests use it to change a source mid-copy.
var afterCopy = func(name string) {}

var experimentIDPattern = regexp.MustCompile(`^[A-Za-z0-9]+([._-][A-Za-z0-9]+)*$`)

// Archive copies the Trader runs a report names from the ingress into an
// experiment package in the experiment's worktree, byte for byte, with
// experiment.json, archive.json and checksums.sha256.
//
// A run is copied only when its run.json reports a terminal status; a run that is
// still running, or has no readable run.json, stops the archive. A run with no
// directory, or a finished run lacking a journal, is recorded as missing, so the
// package reads partial. A source file that changes while it is read stops the
// archive.
//
// A package that already carries the report's id is completed: only files it
// lacks are added. That needs the same experiment.json bytes, every listed file
// still verifying and no archive.json yet; nothing in it is changed, and
// checksums.sha256 keeps its lines and gains lines for the added files.
//
// The package is assembled in a staging directory beside its place, checked by the
// reader, and then renamed into place, so a failure leaves the repository as it
// was. The returned experiment is the package as the reader now sees it.
func Archive(req ArchiveRequest) (Experiment, ArchiveRecord, error) {
	var none Experiment
	var rec ArchiveRecord
	fail := func(err error) (Experiment, ArchiveRecord, error) {
		return none, ArchiveRecord{}, fmt.Errorf("portfolio: archive: %w", err)
	}
	report, runIDs, err := parseReport(req.Report)
	if err != nil {
		return fail(err)
	}
	if err := checkWorktree(req.Portfolio, req.Worktree, report.ID); err != nil {
		return fail(err)
	}
	dest, err := Open(req.Worktree)
	if err != nil {
		return fail(err)
	}
	defer dest.Close()
	existing, err := dest.target(report, req)
	if err != nil {
		return fail(err)
	}
	final := req.Path
	if existing != nil {
		final = existing.Path
	}
	source, err := filepath.Abs(filepath.Join(req.Portfolio, filepath.FromSlash(IngressDir)))
	if err != nil {
		return fail(err)
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	rec = ArchiveRecord{SchemaVersion: ArchiveSchemaVersion, ExperimentID: report.ID, PackagePath: final,
		ArchivedAt: now.UTC(), Source: source, CompletesExisting: existing != nil}

	parent := path.Dir(final)
	if err := dest.root.MkdirAll(parent, 0o755); err != nil {
		return fail(err)
	}
	stage := path.Join(parent, stagingPrefix+report.ID)
	if err := dest.root.Mkdir(stage, 0o755); err != nil {
		return fail(fmt.Errorf("staging directory %s (remove one left by an interrupted archive): %w", stage, err))
	}
	defer dest.root.RemoveAll(stage) // gone already after a successful rename

	if existing != nil {
		err = dest.copyTree(existing.Path, stage)
	} else {
		err = dest.create(path.Join(stage, ManifestFile), bytes.NewReader(req.Report))
	}
	if err != nil {
		return fail(err)
	}
	ingress, err := openIngress(source)
	if err != nil {
		return fail(err)
	}
	if ingress != nil {
		defer ingress.Close()
	}
	var added []string
	for _, id := range runIDs {
		run, copied, err := dest.archiveRun(ingress, stage, id, existing)
		if err != nil {
			return fail(fmt.Errorf("run %s: %w", id, err))
		}
		rec.Runs = append(rec.Runs, run)
		added = append(added, copied...)
	}
	if len(added) == 0 {
		return fail(fmt.Errorf("no new run files for %s in %s: is Trader's results root the ingress?", report.ID, source))
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := dest.create(path.Join(stage, ArchiveFile), bytes.NewReader(append(data, '\n'))); err != nil {
		return fail(err)
	}
	if existing != nil {
		err = dest.extendChecksums(stage, append(added, ArchiveFile))
	} else {
		err = dest.writeChecksums(stage)
	}
	if err != nil {
		return fail(err)
	}
	if staged := dest.load(stage); len(staged.Problems) > 0 || staged.Evidence == Untrusted {
		return fail(fmt.Errorf("the assembled package does not verify: %s", strings.Join(staged.Problems, "; ")))
	}
	if err := dest.install(stage, final, existing != nil); err != nil {
		return fail(err)
	}
	return dest.load(final), rec, nil
}

// parseReport reads what archiving needs from a report: its id and the distinct,
// valid RunIDs it names.
func parseReport(data []byte) (wfa.Report, []string, error) {
	var report wfa.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return report, nil, fmt.Errorf("report: %w", err)
	}
	if !experimentIDPattern.MatchString(report.ID) || strings.HasSuffix(report.ID, ".lock") {
		return report, nil, fmt.Errorf("report: id %q cannot name a branch and a directory", report.ID)
	}
	var ids []string
	for _, run := range report.Runs() {
		if run.Outcome == nil || run.Outcome.RunID == "" {
			continue
		}
		id := run.Outcome.RunID
		if !ValidRunID(id) {
			return report, nil, fmt.Errorf("report: invalid RunID %q", id)
		}
		if slices.Contains(ids, id) {
			return report, nil, fmt.Errorf("report: RunID %s appears twice", id)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return report, nil, errors.New("report: it names no Trader run")
	}
	return report, ids, nil
}

// target finds what is to be written: the existing package to complete, or nil
// for a new package at req.Path, after checking that either may be.
func (r *Repo) target(report wfa.Report, req ArchiveRequest) (*Experiment, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}
	var found []Experiment
	for _, e := range all {
		if e.ID == report.ID {
			found = append(found, e)
		}
	}
	if len(found) > 1 {
		return nil, fmt.Errorf("experiment id %q is used by %d packages", report.ID, len(found))
	}
	if len(found) == 0 {
		return nil, r.checkNewPath(req.Path, report)
	}
	e := found[0]
	switch {
	case req.Path != "" && req.Path != e.Path:
		return nil, fmt.Errorf("experiment %s is already at %s, not %s", e.ID, e.Path, req.Path)
	case e.Manifest != Verified || e.Evidence == Untrusted:
		return nil, fmt.Errorf("%s does not verify and cannot be completed: %s", e.Path, strings.Join(e.Problems, "; "))
	case e.Archived:
		return nil, fmt.Errorf("%s already has %s: an archived package is never changed", e.Path, ArchiveFile)
	case e.Evidence == Complete:
		return nil, fmt.Errorf("%s is already complete", e.Path)
	}
	old, _, _ := r.readChecked(e.Path, ManifestFile, e.sums)
	if !bytes.Equal(old, req.Report) {
		return nil, fmt.Errorf("%s holds a different %s than the report given", e.Path, ManifestFile)
	}
	return &e, nil
}

// checkNewPath accepts backtests/<strategy>/<INSTRUMENT>/<Interval>/<experiment>/<run>
// when nothing is there and it is not inside another package.
func (r *Repo) checkNewPath(p string, report wfa.Report) error {
	if p == "" {
		return fmt.Errorf("experiment %s is new: give its package path", report.ID)
	}
	seg := strings.Split(p, "/")
	if !localPath(p) || len(seg) != 6 || seg[0] != BacktestsDir ||
		slices.ContainsFunc(seg, func(s string) bool { return s == "" || strings.HasPrefix(s, ".") }) {
		return fmt.Errorf("package path %q is not %s/<strategy>/<INSTRUMENT>/<Interval>/<experiment>/<run>", p, BacktestsDir)
	}
	if seg[2] != report.Spec.Instrument || !strings.EqualFold(seg[3], report.Spec.Interval) {
		return fmt.Errorf("package path %q does not match the report's %s %s", p, report.Spec.Instrument, report.Spec.Interval)
	}
	for i := 1; i <= len(seg); i++ {
		at := strings.Join(seg[:i], "/")
		fi, err := r.root.Lstat(at)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case i == len(seg):
			return fmt.Errorf("%s already exists", p)
		case !fi.IsDir():
			return fmt.Errorf("%s is not a directory", at)
		}
		if _, err := r.root.Lstat(path.Join(at, ManifestFile)); err == nil {
			return fmt.Errorf("%s is inside the package %s", p, at)
		}
	}
	return nil
}

// openIngress opens the ingress directory, or returns nil when there is none.
func openIngress(dir string) (*Repo, error) {
	r, err := Open(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return r, err
}

// archiveRun adds what the package lacks of one run to the staging directory, and
// returns the run's record and the package-relative names of the files copied.
// Files the package already has must equal the ingress's, when it has them too.
func (r *Repo) archiveRun(ingress *Repo, stage, id string, existing *Experiment) (ArchivedRun, []string, error) {
	out := ArchivedRun{RunID: id, Files: []ArchivedFile{}}
	have := map[string]string{} // artifact name to the checksum already archived
	if existing != nil {
		run, err := existing.run(id)
		if err != nil {
			return out, nil, err
		}
		for _, f := range run.Files {
			if f.State == Verified {
				have[f.Name] = existing.sums[path.Join(RunsDir, id, f.Name)]
			}
		}
		if run.Record != nil {
			out.TraderStatus = run.Record.Status
		}
	}

	present := false
	if ingress != nil {
		switch fi, err := ingress.root.Lstat(id); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return out, nil, err
		case !fi.IsDir():
			return out, nil, errors.New("the ingress entry is not a directory")
		default:
			present = true
		}
	}
	var record []byte
	if present {
		// run.json decides whether the run is over, so it is read first.
		var err error
		if record, out.TraderStatus, err = ingress.finalizedRecord(id); err != nil {
			return out, nil, err
		}
	}

	var copied []string
	for _, name := range Artifacts {
		rel := path.Join(RunsDir, id, name)
		src := path.Join(id, name)
		inSource := false
		if present {
			_, err := ingress.root.Lstat(src)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return out, nil, err
			}
			inSource = err == nil
		}
		want, archived := have[name]
		switch {
		case archived:
			if inSource {
				if state, detail := ingress.check(src, want, true); state != Verified {
					return out, nil, fmt.Errorf("%s in the ingress is not the archived one: %s%s", name, state, suffix(detail))
				}
			}
			size, err := r.size(path.Join(stage, rel))
			if err != nil {
				return out, nil, err
			}
			out.Files = append(out.Files, ArchivedFile{Name: name, SHA256: want, Bytes: size, Origin: OriginArchived})
		case inSource:
			if err := r.root.MkdirAll(path.Dir(path.Join(stage, rel)), 0o755); err != nil {
				return out, nil, err
			}
			sum, size, err := r.copyStable(ingress, src, path.Join(stage, rel))
			if err != nil {
				return out, nil, err
			}
			if name == RunFile && sum != sumOf(record) {
				return out, nil, fmt.Errorf("%s changed while it was archived", name)
			}
			out.Files = append(out.Files, ArchivedFile{Name: name, SHA256: sum, Bytes: size, Origin: OriginCopied})
			copied = append(copied, rel)
		default:
			out.Missing = append(out.Missing, name)
		}
	}
	if present {
		again, _, err := ingress.finalizedRecord(id)
		if err != nil || !bytes.Equal(again, record) {
			return out, nil, fmt.Errorf("%s changed while the run was archived", RunFile)
		}
	}
	return out, copied, nil
}

// finalizedRecord reads a run's run.json from the ingress and refuses it unless it
// is this run's and reports a terminal status. Trader closes the journals before
// it writes that status.
func (r *Repo) finalizedRecord(id string) ([]byte, string, error) {
	f, err := r.openRegular(path.Join(id, RunFile))
	if err != nil {
		return nil, "", fmt.Errorf("not finalized: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, "", fmt.Errorf("not finalized: %s: %w", RunFile, err)
	}
	if rec.RunID != id {
		return nil, "", fmt.Errorf("%s is of run %q", RunFile, rec.RunID)
	}
	if !Terminal[rec.Status] {
		return nil, "", fmt.Errorf("not finalized: status %q (a crashed run stays %q)", rec.Status, "running")
	}
	return data, rec.Status, nil
}

// copyStable copies one source file to a new file, hashing the bytes as they are
// read, and fails if the source is not the same file, size and modification time
// afterwards.
func (r *Repo) copyStable(src *Repo, from, to string) (string, int64, error) {
	before, err := src.root.Lstat(from)
	if err != nil {
		return "", 0, err
	}
	f, err := src.openRegular(from)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n := &counter{}
	if err := r.create(to, io.TeeReader(f, io.MultiWriter(h, n))); err != nil {
		return "", 0, err
	}
	afterCopy(from)
	after, err := src.root.Lstat(from)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) || n.n != before.Size() {
		return "", 0, fmt.Errorf("%s changed while it was copied", from)
	}
	return hex.EncodeToString(h.Sum(nil)), n.n, nil
}

type counter struct{ n int64 }

func (c *counter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// create writes a new file; it never replaces one.
func (r *Repo) create(name string, content io.Reader) error {
	f, err := r.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (r *Repo) size(name string) (int64, error) {
	fi, err := r.root.Lstat(name)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// files lists every file under dir, relative to it, in lexical order. Anything but
// a regular file or a directory is an error.
func (r *Repo) files(dir string) ([]string, error) {
	var out []string
	err := fs.WalkDir(r.root.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: %w", p, errNotRegular)
		}
		out = append(out, strings.TrimPrefix(p, dir+"/"))
		return nil
	})
	return out, err
}

// copyTree copies a package's files, byte for byte, into dir.
func (r *Repo) copyTree(from, dir string) error {
	names, err := r.files(from)
	if err != nil {
		return err
	}
	for _, name := range names {
		to := path.Join(dir, name)
		if err := r.root.MkdirAll(path.Dir(to), 0o755); err != nil {
			return err
		}
		f, err := r.openRegular(path.Join(from, name))
		if err != nil {
			return err
		}
		err = r.create(to, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// sumLines returns sha256sum lines for the named files of the package at dir.
func (r *Repo) sumLines(dir string, names []string) ([]byte, error) {
	slices.Sort(names)
	var out bytes.Buffer
	for _, name := range names {
		f, err := r.openRegular(path.Join(dir, name))
		if err != nil {
			return nil, err
		}
		sum, err := hashOf(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "%s  %s\n", sum, name)
	}
	return out.Bytes(), nil
}

// writeChecksums lists every file of a new package but the editable ones.
func (r *Repo) writeChecksums(dir string) error {
	names, err := r.files(dir)
	if err != nil {
		return err
	}
	names = slices.DeleteFunc(names, func(n string) bool { return n == ChecksumFile || n == ReportFile || n == NotesFile })
	lines, err := r.sumLines(dir, names)
	if err != nil {
		return err
	}
	return r.create(path.Join(dir, ChecksumFile), bytes.NewReader(lines))
}

// extendChecksums keeps a completed package's checksum lines byte for byte and
// appends lines for the added files, replacing a line only where one already
// listed an added file, which must then match it.
func (r *Repo) extendChecksums(dir string, added []string) error {
	name := path.Join(dir, ChecksumFile)
	f, err := r.openRegular(name)
	if err != nil {
		return err
	}
	old, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return err
	}
	listed, err := r.checksums(dir)
	if err != nil {
		return err
	}
	var fresh []string
	for _, n := range added {
		want, ok := listed[n]
		if !ok {
			fresh = append(fresh, n)
			continue
		}
		if state, detail := r.check(path.Join(dir, n), want, true); state != Verified {
			return fmt.Errorf("%s was listed in %s with another checksum: %s%s", n, ChecksumFile, state, suffix(detail))
		}
	}
	lines, err := r.sumLines(dir, fresh)
	if err != nil {
		return err
	}
	if len(old) > 0 && old[len(old)-1] != '\n' {
		old = append(old, '\n')
	}
	if err := r.root.Remove(name); err != nil { // the staging copy, not the package's
		return err
	}
	return r.create(name, bytes.NewReader(append(old, lines...)))
}

// install renames the staged package into its place. A package being completed is
// moved aside first and removed only once the new one is in place.
func (r *Repo) install(stage, final string, replace bool) error {
	if !replace {
		if _, err := r.root.Lstat(final); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s appeared while it was being archived", final)
		}
		return r.root.Rename(stage, final)
	}
	aside := stage + ".old"
	if err := r.root.Rename(final, aside); err != nil {
		return err
	}
	if err := r.root.Rename(stage, final); err != nil {
		return errors.Join(err, r.root.Rename(aside, final))
	}
	return r.root.RemoveAll(aside)
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
