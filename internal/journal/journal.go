// Package journal is Investor's append-only research journal, defined by
// docs/research-journal.md. Records are immutable: a correction is a new
// revision that supersedes the old one, and old revisions are never rewritten.
package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SchemaVersion is the current record schema version.
const SchemaVersion = 1

var (
	// ErrNotFound means the requested record does not exist.
	ErrNotFound = errors.New("journal record not found")
	// ErrConflict means a record contradicts an existing one: the same
	// identity with different content, a reused revision ID, or a revision
	// that does not follow its predecessor.
	ErrConflict = errors.New("journal conflict")
)

// Record is one immutable revision of a logical journal object.
//
// ID is the stable logical identity; Revision counts up from 1; RevisionID
// uniquely names this exact revision across the whole journal. Re-delivering
// the same revision keeps all three, and a correction keeps ID, increments
// Revision, takes a new RevisionID and names its predecessor in
// SupersedesRevisionID. Times are UTC with nanosecond precision.
type Record struct {
	ID                   string          `json:"id"`
	Kind                 string          `json:"kind"`
	Revision             int             `json:"revision"`
	RevisionID           string          `json:"revision_id"`
	SchemaVersion        int             `json:"schema_version"`
	CreatedAt            time.Time       `json:"created_at"`  // When the represented event happened or began.
	RecordedAt           time.Time       `json:"recorded_at"` // When the journal stored it; set by Append if zero.
	SupersedesRevisionID string          `json:"supersedes_revision_id,omitempty"`
	Payload              json.RawMessage `json:"payload"`
}

// Journal is the narrow storage interface. Implementations must make Append
// durable before returning and must never modify or delete stored records.
type Journal interface {
	// Append stores r. Re-delivering an identical record is a no-op;
	// anything that contradicts stored records returns ErrConflict.
	Append(r Record) error
	// Get returns one exact revision.
	Get(kind, id string, revision int) (Record, error)
	// Latest returns the highest stored revision of a logical object.
	Latest(kind, id string) (Record, error)
	// List returns the logical IDs of a kind, sorted.
	List(kind string) ([]string, error)
}

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// validate checks a record before it is stored.
func (r Record) validate() error {
	switch {
	case !kindPattern.MatchString(r.Kind):
		return fmt.Errorf("invalid kind %q", r.Kind)
	case r.ID == "" || strings.TrimSpace(r.ID) != r.ID || len(r.ID) > 200:
		return fmt.Errorf("invalid id %q", r.ID)
	case r.RevisionID == "" || strings.TrimSpace(r.RevisionID) != r.RevisionID || len(r.RevisionID) > 220:
		return fmt.Errorf("invalid revision_id %q", r.RevisionID)
	case r.Revision < 1:
		return fmt.Errorf("revision must be at least 1, got %d", r.Revision)
	case r.SchemaVersion < 1:
		return fmt.Errorf("schema_version must be at least 1, got %d", r.SchemaVersion)
	case r.CreatedAt.IsZero():
		return errors.New("created_at is required")
	case r.Revision == 1 && r.SupersedesRevisionID != "":
		return errors.New("revision 1 cannot supersede another revision")
	case r.Revision > 1 && r.SupersedesRevisionID == "":
		return fmt.Errorf("revision %d must name supersedes_revision_id", r.Revision)
	case !json.Valid(r.Payload):
		return errors.New("payload must be valid JSON")
	}
	return nil
}

// normalized returns r with UTC times, as stored.
func (r Record) normalized() Record {
	r.CreatedAt = r.CreatedAt.UTC()
	r.RecordedAt = r.RecordedAt.UTC()
	return r
}

// sameRevision reports whether two records are the same revision, ignoring
// RecordedAt (when the journal happened to store it) and payload formatting.
func sameRevision(a, b Record) bool {
	var pa, pb bytes.Buffer
	if json.Compact(&pa, a.Payload) != nil || json.Compact(&pb, b.Payload) != nil {
		return false
	}
	return a.ID == b.ID && a.Kind == b.Kind && a.Revision == b.Revision &&
		a.RevisionID == b.RevisionID && a.SchemaVersion == b.SchemaVersion &&
		a.SupersedesRevisionID == b.SupersedesRevisionID &&
		a.CreatedAt.Equal(b.CreatedAt) && bytes.Equal(pa.Bytes(), pb.Bytes())
}
