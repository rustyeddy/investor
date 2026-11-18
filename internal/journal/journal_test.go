package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 17, 12, 30, 45, 123456789, time.UTC)

func rec(kind, id string, rev int, payload string) Record {
	r := Record{ID: id, Kind: kind, Revision: rev, RevisionID: id + "@" + string(rune('0'+rev)), SchemaVersion: SchemaVersion, CreatedAt: t0, Payload: json.RawMessage(payload)}
	if rev > 1 {
		r.SupersedesRevisionID = id + "@" + string(rune('0'+rev-1))
	}
	return r
}

// compact strips formatting: stored payloads are indented for readability.
func compact(p json.RawMessage) string {
	var b bytes.Buffer
	_ = json.Compact(&b, p)
	return b.String()
}

func open(t *testing.T, dir string) *Files {
	t.Helper()
	j, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestAppendGetLatestListAndRestart(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir)
	for _, r := range []Record{rec("run", "r1", 1, `{"status":"running"}`), rec("run", "r1", 2, `{"status":"failed"}`), rec("run", "r0", 1, `{}`), rec("step", "r1/analyst", 1, `{}`)} {
		if err := j.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	// Restart: a fresh Journal over the same directory sees everything.
	j = open(t, dir)
	if got, err := j.Latest("run", "r1"); err != nil || got.Revision != 2 || compact(got.Payload) != `{"status":"failed"}` {
		t.Fatalf("latest = %+v, %v", got, err)
	}
	// The superseded revision is retained, not overwritten.
	if got, err := j.Get("run", "r1", 1); err != nil || got.Revision != 1 || !strings.Contains(string(got.Payload), "running") {
		t.Fatalf("revision 1 = %+v, %v", got, err)
	}
	if ids, err := j.List("run"); err != nil || len(ids) != 2 || ids[0] != "r0" || ids[1] != "r1" {
		t.Fatalf("list = %v, %v", ids, err)
	}
	if ids, _ := j.List("step"); len(ids) != 1 || ids[0] != "r1/analyst" {
		t.Fatalf("ids with slashes must round-trip: %v", ids)
	}
	if ids, err := j.List("order"); err != nil || len(ids) != 0 {
		t.Fatalf("empty kind: %v, %v", ids, err)
	}
}

func TestNotFound(t *testing.T) {
	j := open(t, t.TempDir())
	if _, err := j.Get("run", "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if _, err := j.Latest("run", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Latest: %v", err)
	}
}

func TestExactTimestampsAreUTCWithNanoseconds(t *testing.T) {
	j := open(t, t.TempDir())
	r := rec("run", "r1", 1, `{}`)
	r.CreatedAt = t0.In(time.FixedZone("X", -5*3600)) // Same instant, other zone.
	r.RecordedAt = t0.Add(time.Second)
	if err := j.Append(r); err != nil {
		t.Fatal(err)
	}
	got, err := j.Get("run", "r1", 1)
	if err != nil || !got.CreatedAt.Equal(t0) || got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond() != 123456789 || !got.RecordedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("got %+v, %v", got, err)
	}
	data, _ := os.ReadFile(filepath.Join(j.dir, "records", "run", "r1", "00000001.json"))
	if !strings.Contains(string(data), `"2026-11-17T12:30:45.123456789Z"`) {
		t.Fatalf("stored timestamp not exact UTC RFC 3339: %s", data)
	}
	// RecordedAt is set when omitted.
	if err := j.Append(rec("run", "r2", 1, `{}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := j.Get("run", "r2", 1); got.RecordedAt.IsZero() {
		t.Fatal("RecordedAt not set")
	}
}

func TestRedeliveryIsIdempotentAndConflictsAreRejected(t *testing.T) {
	j := open(t, t.TempDir())
	r := rec("run", "r1", 1, `{"a": 1}`)
	if err := j.Append(r); err != nil {
		t.Fatal(err)
	}
	first, _ := j.Get("run", "r1", 1)
	// Identical redelivery (even re-formatted payload, later RecordedAt): no-op.
	again := r
	again.Payload = json.RawMessage(`{"a":1}`)
	again.RecordedAt = time.Now()
	if err := j.Append(again); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if got, _ := j.Get("run", "r1", 1); !got.RecordedAt.Equal(first.RecordedAt) {
		t.Fatal("redelivery changed the stored record")
	}
	// Same identity, different content.
	changed := r
	changed.Payload = json.RawMessage(`{"a":2}`)
	if err := j.Append(changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed content: %v", err)
	}
	// Reused revision ID on a different record must not create anything.
	other := rec("run", "r2", 1, `{}`)
	other.RevisionID = r.RevisionID
	if err := j.Append(other); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused revision_id: %v", err)
	}
	if _, err := j.Get("run", "r2", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conflicting append left a record: %v", err)
	}
}

func TestRevisionChainIsValidated(t *testing.T) {
	j := open(t, t.TempDir())
	if err := j.Append(rec("run", "r1", 2, `{}`)); err == nil || !strings.Contains(err.Error(), "predecessor") {
		t.Fatalf("revision 2 without 1: %v", err)
	}
	if err := j.Append(rec("run", "r1", 1, `{}`)); err != nil {
		t.Fatal(err)
	}
	bad := rec("run", "r1", 2, `{}`)
	bad.SupersedesRevisionID = "something-else"
	if err := j.Append(bad); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong supersedes: %v", err)
	}
	for name, mutate := range map[string]func(*Record){
		"revision 1 supersedes": func(r *Record) { r.SupersedesRevisionID = "x" },
		"bad kind":              func(r *Record) { r.Kind = "Run!" },
		"empty id":              func(r *Record) { r.ID = "" },
		"zero revision":         func(r *Record) { r.Revision = 0 },
		"no created_at":         func(r *Record) { r.CreatedAt = time.Time{} },
		"bad payload":           func(r *Record) { r.Payload = json.RawMessage(`{`) },
		"no revision_id":        func(r *Record) { r.RevisionID = "" },
	} {
		r := rec("run", "v", 1, `{}`)
		mutate(&r)
		if err := j.Append(r); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestIDsCannotEscapeTheJournal(t *testing.T) {
	dir := t.TempDir()
	j := open(t, filepath.Join(dir, "j"))
	for _, id := range []string{"..", ".", "../../escape"} {
		r := rec("run", id, 1, `{}`)
		r.RevisionID = "rev-" + strings.ReplaceAll(id, "/", "_")
		_ = j.Append(r) // `..` and `.` are rejected; escaped slashes stay inside.
	}
	var found []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = append(found, p)
		}
		return nil
	})
	for _, p := range found {
		if !strings.HasPrefix(p, filepath.Join(dir, "j")+string(os.PathSeparator)) {
			t.Fatalf("file outside journal: %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); err == nil {
		t.Fatal("path traversal succeeded")
	}
}

func TestRecoversFromCrashBetweenIndexAndRecord(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir)
	r := rec("run", "r1", 1, `{}`)
	if err := j.Append(r); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the revision index was written but before the record.
	if err := os.Remove(filepath.Join(dir, "records", "run", "r1", "00000001.json")); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(r); err != nil {
		t.Fatalf("retry after crash: %v", err)
	}
	got, err := j.Get("run", "r1", 1)
	if err != nil || got.RevisionID != r.RevisionID {
		t.Fatalf("got %+v, %v", got, err)
	}
	a, _ := os.Stat(filepath.Join(dir, "records", "run", "r1", "00000001.json"))
	b, _ := os.Stat(filepath.Join(dir, "revisions", "r1@1.json"))
	if !os.SameFile(a, b) {
		t.Fatal("record and revision index must be the same file")
	}
}

func TestFormatVersion(t *testing.T) {
	dir := t.TempDir()
	open(t, dir)
	data, _ := os.ReadFile(filepath.Join(dir, "journal.json"))
	if !strings.Contains(string(data), `"journal_schema_version":1`) {
		t.Fatalf("marker = %s", data)
	}
	open(t, dir) // Reopening an existing journal works.
	if err := os.WriteFile(filepath.Join(dir, "journal.json"), []byte(`{"journal_schema_version":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "unsupported journal schema version 99") {
		t.Fatalf("future version: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "journal.json"), []byte(`not json`), 0o644)
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt marker accepted")
	}
}

func TestWriteFailureIsReportedAndLeavesNoRecord(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	dir := t.TempDir()
	j := open(t, dir)
	if err := j.Append(rec("run", "r1", 1, `{}`)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, ".tmp"), filepath.Join(dir, "records")} {
		if err := os.Chmod(p, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(p, 0o755)
	}
	if err := j.Append(rec("run", "r2", 1, `{}`)); err == nil {
		t.Fatal("expected write failure")
	}
	for _, p := range []string{filepath.Join(dir, ".tmp"), filepath.Join(dir, "records")} {
		os.Chmod(p, 0o755)
	}
	if _, err := j.Get("run", "r2", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed write left a record: %v", err)
	}
	if _, err := j.Get("run", "r1", 1); err != nil {
		t.Fatalf("existing record damaged: %v", err)
	}
}

func TestDetectsRecordFilesThatDoNotMatchTheirPath(t *testing.T) {
	dir := t.TempDir()
	j := open(t, dir)
	_ = j.Append(rec("run", "r1", 1, `{}`))
	_ = j.Append(rec("run", "r2", 1, `{}`))
	data, _ := os.ReadFile(filepath.Join(dir, "records", "run", "r2", "00000001.json"))
	// Replace r1's file with r2's content (corruption).
	p := filepath.Join(dir, "records", "run", "r1", "00000001.json")
	os.Remove(p)
	os.WriteFile(p, data, 0o644)
	if _, err := j.Get("run", "r1", 1); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("err = %v", err)
	}
}
