package journal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rustyeddy/investor/internal/firm"
)

// Observer records a firm run in a Journal as it progresses:
//
//	run  <run id>                      revision 1 "running" at start, revision 2
//	                                   (superseding 1) with the terminal outcome
//	step <run id>/<agent id>           one record per agent step, including
//	                                   failures and stops
//
// A run whose process died stays at revision 1 with status "running", which is
// how interrupted runs remain visible. Revision IDs are "<id>@<revision>", so
// retried deliveries are idempotent.
type Observer struct{ J Journal }

var _ firm.Observer = Observer{}

// NewObserver returns an Observer that writes to j.
func NewObserver(j Journal) Observer { return Observer{J: j} }

// RevisionID is the deterministic revision ID used for a logical ID and revision.
func RevisionID(id string, revision int) string { return fmt.Sprintf("%s@%d", id, revision) }

func (o Observer) RunStarted(run firm.Run) error {
	return o.append("run", run.ID, 1, run.StartedAt, run)
}

func (o Observer) StepFinished(runID string, step firm.Step) error {
	return o.append("step", runID+"/"+step.AgentID, 1, step.StartedAt, struct {
		RunID string `json:"run_id"`
		firm.Step
	}{runID, step})
}

func (o Observer) RunFinished(run firm.Run) error {
	return o.append("run", run.ID, 2, run.FinishedAt, run)
}

func (o Observer) append(kind, id string, revision int, at time.Time, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r := Record{ID: id, Kind: kind, Revision: revision, RevisionID: RevisionID(id, revision), SchemaVersion: SchemaVersion, CreatedAt: at, Payload: data}
	if revision > 1 {
		r.SupersedesRevisionID = RevisionID(id, revision-1)
	}
	return o.J.Append(r)
}
