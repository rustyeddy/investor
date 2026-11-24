package market

import (
	"context"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// Fixture is a deterministic in-memory Source for tests and offline runs. It
// serves Bars filtered by the request and reports Coverage as given; it
// invents nothing. Now stamps RetrievedAt (zero time if unset).
type Fixture struct {
	Series   map[string][]marketdata.Bar // Key: "EURUSD/h1".
	Coverage Coverage
	Now      time.Time
}

// FixtureKey is the Fixture.Bars key for an instrument and interval.
func FixtureKey(instrument, interval string) string {
	return strings.ToUpper(instrument) + "/" + interval
}

// Bars implements Source.
func (f Fixture) Bars(ctx context.Context, r Request) (Bars, error) {
	if err := ctx.Err(); err != nil {
		return Bars{}, err
	}
	if err := r.Validate(); err != nil {
		return Bars{}, err
	}
	var out []marketdata.Bar
	for _, b := range f.Series[FixtureKey(r.Instrument, r.Interval)] {
		if b.Time.Before(r.From) || !b.Time.Before(r.To) {
			continue
		}
		if !r.AsOf.IsZero() && b.AvailableAt.After(r.AsOf) {
			continue
		}
		b.Instrument = strings.ToUpper(r.Instrument)
		out = append(out, b)
	}
	if len(out) == 0 {
		return Bars{}, ErrNoData
	}
	return finish(r, "fixture", out, f.Coverage, f.Now)
}
