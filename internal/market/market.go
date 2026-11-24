// Package market is how Investor reads market data. Trader owns the data
// (acquisition, cleaning, canonical format); Investor consumes it through the
// Source interface, implemented by a REST client of Trader's API and by an
// in-memory fixture for tests and offline runs. See docs/trader-marketdata.md.
package market

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// Errors, classifiable with errors.Is. The REST client wraps Trader's error
// body in an *APIError that matches the sentinel for its code.
var (
	ErrNoData       = errors.New("market: no data for the range")
	ErrIncomplete   = errors.New("market: range is not complete")
	ErrUnauthorized = errors.New("market: unauthorized")
	ErrInvalid      = errors.New("market: invalid request or response")
)

// Intervals Trader serves.
var Intervals = []string{"h1", "d1"}

// Request selects bars by opening time: From inclusive, To exclusive.
type Request struct {
	Instrument string // e.g. EURUSD.
	Interval   string // h1 or d1.
	From, To   time.Time
	// AsOf, when set, keeps only bars with AvailableAt <= AsOf, so a replay
	// cannot see the future. Leave it zero only when the range ends in the past.
	AsOf time.Time
	// Strict refuses an incomplete range (ErrIncomplete) instead of returning it
	// with its coverage.
	Strict bool
}

// Validate reports whether the request is well formed.
func (r Request) Validate() error {
	switch {
	case strings.TrimSpace(r.Instrument) == "":
		return fmt.Errorf("%w: instrument is required", ErrInvalid)
	case r.Interval != "h1" && r.Interval != "d1":
		return fmt.Errorf("%w: interval %q (want h1 or d1)", ErrInvalid, r.Interval)
	case r.From.IsZero() || r.To.IsZero() || !r.From.Before(r.To):
		return fmt.Errorf("%w: from must be before to", ErrInvalid)
	}
	return nil
}

// Gap is a run of missing bars; Kind is closure, known_hole or unexplained.
type Gap struct {
	Kind  string    `json:"kind"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"` // Exclusive.
	Bars  int       `json:"bars"`
}

// Month is a month that is not fully loaded; State is bad, missing or stale.
type Month struct {
	Year   int    `json:"year"`
	Month  int    `json:"month"`
	State  string `json:"state"`
	Good   int    `json:"good"`
	Bad    int    `json:"bad"`
	Reason string `json:"reason,omitempty"`
}

// Coverage says what Trader did not have for the range. Missing data is never
// hidden: a consumer must look at Complete before drawing conclusions.
type Coverage struct {
	Complete bool    `json:"complete"`
	Months   []Month `json:"months"`
	Gaps     []Gap   `json:"gaps"`
}

// Bars is the answer to a Request plus the provenance a decision must record.
type Bars struct {
	Source      string           `json:"source"`
	Instrument  string           `json:"instrument"`
	Interval    string           `json:"interval"`
	From        time.Time        `json:"from"`
	To          time.Time        `json:"to"`
	AsOf        time.Time        `json:"as_of,omitzero"`
	RetrievedAt time.Time        `json:"retrieved_at"`
	Fingerprint string           `json:"fingerprint"` // See Fingerprint.
	Coverage    Coverage         `json:"coverage"`
	Bars        []marketdata.Bar `json:"bars"`
}

// MarshalJSON encodes the bars in Investor's stable form (see encodeBar).
func (b Bars) MarshalJSON() ([]byte, error) {
	type plain Bars
	enc := make([]wireBar, len(b.Bars))
	for i, x := range b.Bars {
		enc[i] = encodeBar(x)
	}
	return json.Marshal(struct {
		plain
		Bars []wireBar `json:"bars"`
	}{plain(b), enc})
}

// Source supplies bars. Implementations must return only completed bars in
// ascending opening order and must not invent or fill data.
type Source interface {
	Bars(ctx context.Context, r Request) (Bars, error)
}

// Fingerprint is a SHA-256 over the bars' exact content (opening and
// availability times, volume and every price), so a result can be shown to be
// the same data later. It does not depend on how the bars were fetched.
func Fingerprint(bars []marketdata.Bar) string {
	h := sha256.New()
	for _, b := range bars {
		fmt.Fprintf(h, "%s %s %d", b.Time.UTC().Format(time.RFC3339Nano), b.AvailableAt.UTC().Format(time.RFC3339Nano), b.Volume)
		for _, s := range []marketdata.OHLC{b.Bid, b.Ask, b.Mid, b.Trade} {
			fmt.Fprintf(h, " %s %s %s %s", s.Open, s.High, s.Low, s.Close)
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// check enforces what every Source promises about its bars: valid, available,
// ascending, inside [from, to), and not later than asOf.
func check(r Request, bars []marketdata.Bar) error {
	var prev time.Time
	for i, b := range bars {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if b.Time.Before(r.From) || !b.Time.Before(r.To) {
			return fmt.Errorf("%w: bar %s is outside the requested range", ErrInvalid, b.Time.Format(time.RFC3339))
		}
		if i > 0 && !b.Time.After(prev) {
			return fmt.Errorf("%w: bar %s is not after %s", ErrInvalid, b.Time.Format(time.RFC3339), prev.Format(time.RFC3339))
		}
		if !r.AsOf.IsZero() && b.AvailableAt.After(r.AsOf) {
			return fmt.Errorf("%w: bar %s was not available at %s (look-ahead)", ErrInvalid, b.Time.Format(time.RFC3339), r.AsOf.Format(time.RFC3339))
		}
		prev = b.Time
	}
	return nil
}

// finish stamps provenance on a checked result.
func finish(r Request, source string, bars []marketdata.Bar, cov Coverage, now time.Time) (Bars, error) {
	if err := check(r, bars); err != nil {
		return Bars{}, err
	}
	if r.Strict && !cov.Complete {
		return Bars{}, fmt.Errorf("%w: %d gap(s), %d month(s) not loaded", ErrIncomplete, len(cov.Gaps), len(cov.Months))
	}
	return Bars{
		Source: source, Instrument: strings.ToUpper(r.Instrument), Interval: r.Interval,
		From: r.From.UTC(), To: r.To.UTC(), AsOf: r.AsOf.UTC(), RetrievedAt: now.UTC(),
		Fingerprint: Fingerprint(bars), Coverage: cov, Bars: bars,
	}, nil
}
