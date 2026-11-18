package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	formatFile = "journal.json"
	tmpDir     = ".tmp"
)

// Files is a Journal stored as one immutable JSON file per record revision:
//
//	<dir>/journal.json                        format version marker
//	<dir>/records/<kind>/<id>/<revision>.json each revision (id is path-escaped)
//	<dir>/revisions/<revision_id>.json        hard link to the same file
//
// A revision file is created by writing a synced temporary file and hard-linking
// it into place, which fails rather than overwrites if the name exists. The
// revisions/ link makes revision IDs globally unique. Files are safe to read
// and diff by hand but must not be edited: edits would break immutability.
type Files struct{ dir string }

var _ Journal = (*Files)(nil)

type format struct {
	JournalSchemaVersion int `json:"journal_schema_version"`
}

// Open opens the journal in dir, creating it if it does not exist. It refuses a
// journal written with a format version it does not understand.
func Open(dir string) (*Files, error) {
	if err := os.MkdirAll(filepath.Join(dir, tmpDir), 0o755); err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	marker := filepath.Join(dir, formatFile)
	data, err := os.ReadFile(marker)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		out, _ := json.Marshal(format{SchemaVersion})
		if err := writeNew(dir, marker, append(out, '\n')); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("open journal: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("open journal: %w", err)
	default:
		var f format
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("open journal: invalid %s: %w", formatFile, err)
		}
		if f.JournalSchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("open journal: unsupported journal schema version %d (this build supports %d); migrate it first", f.JournalSchemaVersion, SchemaVersion)
		}
	}
	return &Files{dir: dir}, nil
}

// escape makes an ID safe as one path element.
func escape(s string) (string, error) {
	e := url.PathEscape(s)
	if e == "" || e == "." || e == ".." {
		return "", fmt.Errorf("invalid name %q", s)
	}
	return e, nil
}

func (f *Files) recordPath(kind, id string, revision int) (string, error) {
	e, err := escape(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(f.dir, "records", kind, e, fmt.Sprintf("%08d.json", revision)), nil
}

func (f *Files) revisionPath(revisionID string) (string, error) {
	e, err := escape(revisionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(f.dir, "revisions", e+".json"), nil
}

func (f *Files) Append(r Record) error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("journal: invalid record: %w", err)
	}
	if r.RecordedAt.IsZero() {
		r.RecordedAt = time.Now()
	}
	r = r.normalized()
	primary, err := f.recordPath(r.Kind, r.ID, r.Revision)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	index, err := f.revisionPath(r.RevisionID)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	if r.Revision > 1 {
		prev, err := f.Get(r.Kind, r.ID, r.Revision-1)
		if err != nil {
			return fmt.Errorf("journal: revision %d of %s %q has no stored predecessor: %w", r.Revision, r.Kind, r.ID, err)
		}
		if prev.RevisionID != r.SupersedesRevisionID {
			return fmt.Errorf("%w: %s %q revision %d supersedes %q, but revision %d is %q", ErrConflict, r.Kind, r.ID, r.Revision, r.SupersedesRevisionID, prev.Revision, prev.RevisionID)
		}
	}
	data, err := json.MarshalIndent(r, "", "\t")
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := f.writeTemp(data)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer os.Remove(tmp)

	// Claim the revision ID first, so a reused ID is rejected before any
	// record is created. If it is already ours (a retry after a crash), link
	// the existing file so both names stay the same file.
	source := tmp
	if err := link(tmp, index); err != nil {
		existing, rerr := readRecord(index)
		if rerr != nil || !sameRevision(existing, r) {
			return fmt.Errorf("%w: revision_id %q is already used by a different record", ErrConflict, r.RevisionID)
		}
		source = index
	}
	if err := link(source, primary); err != nil {
		existing, rerr := readRecord(primary)
		if rerr != nil || !sameRevision(existing, r) {
			return fmt.Errorf("%w: %s %q revision %d already exists with different content", ErrConflict, r.Kind, r.ID, r.Revision)
		}
	}
	return syncDirs(filepath.Dir(primary), filepath.Dir(index))
}

func (f *Files) Get(kind, id string, revision int) (Record, error) {
	if !kindPattern.MatchString(kind) {
		return Record{}, fmt.Errorf("journal: invalid kind %q", kind)
	}
	p, err := f.recordPath(kind, id, revision)
	if err != nil {
		return Record{}, fmt.Errorf("journal: %w", err)
	}
	return f.read(p, kind, id, revision)
}

func (f *Files) read(path, kind, id string, revision int) (Record, error) {
	r, err := readRecord(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, fmt.Errorf("%w: %s %q revision %d", ErrNotFound, kind, id, revision)
	}
	if err != nil {
		return Record{}, fmt.Errorf("journal: %w", err)
	}
	if r.Kind != kind || r.ID != id || r.Revision != revision {
		return Record{}, fmt.Errorf("journal: %s holds %s %q revision %d, not %s %q revision %d (journal corrupted)", path, r.Kind, r.ID, r.Revision, kind, id, revision)
	}
	return r, nil
}

func (f *Files) Latest(kind, id string) (Record, error) {
	if !kindPattern.MatchString(kind) {
		return Record{}, fmt.Errorf("journal: invalid kind %q", kind)
	}
	p, err := f.recordPath(kind, id, 1)
	if err != nil {
		return Record{}, fmt.Errorf("journal: %w", err)
	}
	entries, err := os.ReadDir(filepath.Dir(p))
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, fmt.Errorf("%w: %s %q", ErrNotFound, kind, id)
	}
	if err != nil {
		return Record{}, fmt.Errorf("journal: %w", err)
	}
	latest := 0
	for _, e := range entries {
		if n, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json")); err == nil && strings.HasSuffix(e.Name(), ".json") && n > latest {
			latest = n
		}
	}
	if latest == 0 {
		return Record{}, fmt.Errorf("%w: %s %q", ErrNotFound, kind, id)
	}
	return f.Get(kind, id, latest)
}

func (f *Files) List(kind string) ([]string, error) {
	if !kindPattern.MatchString(kind) {
		return nil, fmt.Errorf("journal: invalid kind %q", kind)
	}
	entries, err := os.ReadDir(filepath.Join(f.dir, "records", kind))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("journal: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		id, err := url.PathUnescape(e.Name())
		if err != nil {
			return nil, fmt.Errorf("journal: bad record directory %q: %w", e.Name(), err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func readRecord(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return r, nil
}

// writeTemp writes data to a new synced file under .tmp and returns its path.
func (f *Files) writeTemp(data []byte) (string, error) {
	t, err := os.CreateTemp(filepath.Join(f.dir, tmpDir), "rec-*")
	if err != nil {
		return "", err
	}
	_, werr := t.Write(data)
	serr := t.Sync()
	cerr := t.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		_ = os.Remove(t.Name())
		return "", err
	}
	return t.Name(), nil
}

// link hard-links src to dst, creating dst's directory. It fails if dst exists.
func link(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Link(src, dst)
}

// writeNew creates dst with data, failing if it exists.
func writeNew(dir, dst string, data []byte) error {
	t, err := os.CreateTemp(filepath.Join(dir, tmpDir), "new-*")
	if err != nil {
		return err
	}
	defer os.Remove(t.Name())
	_, werr := t.Write(data)
	serr := t.Sync()
	cerr := t.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		return err
	}
	if err := os.Link(t.Name(), dst); err != nil {
		return err
	}
	return syncDirs(filepath.Dir(dst))
}

func syncDirs(dirs ...string) error {
	for _, d := range dirs {
		f, err := os.Open(d)
		if err != nil {
			return err
		}
		err = f.Sync()
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
