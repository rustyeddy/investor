package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// SnapshotEntry is the latest completed bar for one instrument and interval,
// or the reason there is none. A failure for one entry never hides the others.
type SnapshotEntry struct {
	Instrument  string          `json:"instrument"`
	Interval    string          `json:"interval"`
	Latest      *marketdata.Bar `json:"latest,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"` // Of the bars read, not just Latest.
	Complete    bool            `json:"complete"`
	Gaps        int             `json:"gaps"`
	Error       string          `json:"error,omitempty"`
}

// MarshalJSON encodes Latest in Investor's stable bar form.
func (e SnapshotEntry) MarshalJSON() ([]byte, error) {
	type plain SnapshotEntry
	var latest *wireBar
	if e.Latest != nil {
		b := encodeBar(*e.Latest)
		latest = &b
	}
	return json.Marshal(struct {
		plain
		Latest *wireBar `json:"latest,omitempty"`
	}{plain(e), latest})
}

// Snapshot is a timestamped observation of a watchlist as of AsOf.
type Snapshot struct {
	AsOf    time.Time       `json:"as_of"`
	Entries []SnapshotEntry `json:"entries"`
}

// lookback is how far back a snapshot reads to find the latest completed bar:
// enough to span a weekend or holiday.
var lookback = map[string]time.Duration{"h1": 4 * 24 * time.Hour, "d1": 10 * 24 * time.Hour}

// TakeSnapshot reads the latest completed D1 and H1 bar of each instrument from
// src as of now. Only bars available at now are used, so no unfinished bar is
// presented as current.
func TakeSnapshot(ctx context.Context, src Source, instruments []string, now time.Time) Snapshot {
	now = now.UTC().Truncate(time.Second)
	snap := Snapshot{AsOf: now, Entries: []SnapshotEntry{}}
	for _, in := range instruments {
		for _, iv := range []string{"d1", "h1"} {
			e := SnapshotEntry{Instrument: in, Interval: iv}
			got, err := src.Bars(ctx, Request{Instrument: in, Interval: iv, From: now.Add(-lookback[iv]), To: now, AsOf: now})
			if err != nil {
				e.Error = err.Error()
			} else {
				e.Latest, e.Fingerprint = &got.Bars[len(got.Bars)-1], got.Fingerprint
				e.Complete, e.Gaps = got.Coverage.Complete, len(got.Coverage.Gaps)
			}
			snap.Entries = append(snap.Entries, e)
		}
	}
	return snap
}
