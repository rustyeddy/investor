package market

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

var t0 = time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

func price(t *testing.T, s string) num.Price {
	t.Helper()
	p, err := num.ParsePrice(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ohlc(t *testing.T, o, h, l, c string) marketdata.OHLC {
	return marketdata.OHLC{Open: price(t, o), High: price(t, h), Low: price(t, l), Close: price(t, c)}
}

// bar is the h1 bar opening i hours after t0, with a valid bid, ask and mid.
func bar(t *testing.T, i int) marketdata.Bar {
	t.Helper()
	bid, ask := ohlc(t, "1.10000", "1.10100", "1.09900", "1.10050"), ohlc(t, "1.10010", "1.10110", "1.09910", "1.10060")
	mid, err := marketdata.MidOf(bid, ask)
	if err != nil {
		t.Fatal(err)
	}
	open := t0.Add(time.Duration(i) * time.Hour)
	return marketdata.Bar{Time: open, AvailableAt: open.Add(time.Hour), Volume: 100 + int64(i), Bid: bid, Ask: ask, Mid: mid}
}

func req() Request {
	return Request{Instrument: "eurusd", Interval: "h1", From: t0, To: t0.Add(24 * time.Hour)}
}

func TestFixtureFiltersAndFingerprints(t *testing.T) {
	f := Fixture{Series: map[string][]marketdata.Bar{"EURUSD/h1": {bar(t, 0), bar(t, 1), bar(t, 2)}}, Coverage: Coverage{Complete: true}, Now: t0}
	r := req()
	r.AsOf = t0.Add(2 * time.Hour) // Bar 1 is available at 02:00; bar 2 is not.
	got, err := f.Bars(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bars) != 2 || got.Instrument != "EURUSD" || got.Source != "fixture" {
		t.Fatalf("got %d bars, %q, %q", len(got.Bars), got.Instrument, got.Source)
	}
	again, _ := f.Bars(context.Background(), r)
	if got.Fingerprint == "" || got.Fingerprint != again.Fingerprint {
		t.Error("fingerprint is not stable")
	}
	r.AsOf = time.Time{}
	all, _ := f.Bars(context.Background(), r)
	if all.Fingerprint == got.Fingerprint {
		t.Error("fingerprint ignores bars")
	}
	r.Instrument = "GBPUSD"
	if _, err := f.Bars(context.Background(), r); !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
}

func TestStrictRefusesIncompleteFixture(t *testing.T) {
	f := Fixture{Series: map[string][]marketdata.Bar{"EURUSD/h1": {bar(t, 0)}}, Coverage: Coverage{Gaps: []Gap{{Kind: "unexplained", Bars: 3}}}}
	if _, err := f.Bars(context.Background(), req()); err != nil {
		t.Fatalf("non-strict: %v", err)
	}
	r := req()
	r.Strict = true
	if _, err := f.Bars(context.Background(), r); !errors.Is(err, ErrIncomplete) {
		t.Errorf("err = %v, want ErrIncomplete", err)
	}
}

func TestRequestValidate(t *testing.T) {
	for name, r := range map[string]Request{
		"no instrument": {Interval: "h1", From: t0, To: t0.Add(time.Hour)},
		"bad interval":  {Instrument: "EURUSD", Interval: "m1", From: t0, To: t0.Add(time.Hour)},
		"reversed":      {Instrument: "EURUSD", Interval: "h1", From: t0.Add(time.Hour), To: t0},
	} {
		if err := r.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func serverBar(b marketdata.Bar) map[string]any {
	o := func(s marketdata.OHLC) map[string]string {
		return map[string]string{"open": s.Open.String(), "high": s.High.String(), "low": s.Low.String(), "close": s.Close.String()}
	}
	return map[string]any{"time": b.Time, "available_at": b.AvailableAt, "volume": b.Volume, "bid": o(b.Bid), "ask": o(b.Ask), "mid": o(b.Mid)}
}

func TestClientFollowsCursorsAndMergesCoverage(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/v1/marketdata/bars" {
			http.Error(w, "bad request", http.StatusTeapot)
			return
		}
		page := map[string]any{"source": "oanda", "instrument": "EURUSD", "interval": "h1"}
		if r.URL.Query().Get("cursor") == "" {
			page["bars"] = []any{serverBar(bar(t, 0)), serverBar(bar(t, 1))}
			page["next_cursor"] = "c1"
			page["coverage"] = map[string]any{"complete": false, "months": []Month{{Year: 2024, Month: 1, State: "bad"}}, "gaps": []Gap{{Kind: "unexplained", Start: t0, End: t0.Add(time.Hour), Bars: 1}}}
		} else {
			page["bars"] = []any{serverBar(bar(t, 2))}
			page["coverage"] = map[string]any{"complete": false, "months": []Month{{Year: 2024, Month: 1, State: "bad"}}, "gaps": []Gap{{Kind: "closure", Bars: 2}}}
		}
		json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/", Token: "secret", Now: func() time.Time { return t0 }}
	r := req()
	r.AsOf = t0.Add(10 * time.Hour)
	got, err := c.Bars(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bars) != 3 || got.Source != "oanda" || got.Bars[0].Instrument != "EURUSD" || got.Fingerprint != Fingerprint(got.Bars) {
		t.Fatalf("got %+v", got)
	}
	if got.Coverage.Complete || len(got.Coverage.Gaps) != 2 || len(got.Coverage.Months) != 1 {
		t.Errorf("coverage = %+v", got.Coverage)
	}
	if len(queries) != 2 {
		t.Fatalf("%d requests", len(queries))
	}
	for _, want := range []string{"as_of=", "instrument=EURUSD", "interval=h1"} {
		if !strings.Contains(queries[0], want) {
			t.Errorf("query %q lacks %q", queries[0], want)
		}
	}
}

func TestClientErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		want   error
	}{
		{404, "no_data", ErrNoData},
		{422, "incomplete", ErrIncomplete},
		{401, "unauthorized", ErrUnauthorized},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": tc.code, "message": "m"}, "coverage": Coverage{}})
		}))
		_, err := (&Client{BaseURL: srv.URL}).Bars(context.Background(), req())
		srv.Close()
		var api *APIError
		if !errors.Is(err, tc.want) || !errors.As(err, &api) || api.Status != tc.status {
			t.Errorf("%s: err = %v", tc.code, err)
		}
	}
}

func TestClientRejectsBadResponses(t *testing.T) {
	late := serverBar(bar(t, 5))
	for name, page := range map[string]map[string]any{
		"look-ahead":  {"bars": []any{late}, "coverage": Coverage{Complete: true}},
		"no coverage": {"bars": []any{serverBar(bar(t, 0))}},
		"outside":     {"bars": []any{serverBar(bar(t, 30))}, "coverage": Coverage{Complete: true}},
		"empty":       {"bars": []any{}, "coverage": Coverage{Complete: true}},
		"stuck":       {"bars": []any{serverBar(bar(t, 0))}, "next_cursor": "x", "coverage": Coverage{Complete: true}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { json.NewEncoder(w).Encode(page) }))
		r := req()
		if name == "look-ahead" {
			r.AsOf = t0.Add(time.Hour)
		}
		_, err := (&Client{BaseURL: srv.URL}).Bars(context.Background(), r)
		srv.Close()
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestClientUnreachable(t *testing.T) {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	if _, err := (&Client{BaseURL: url}).Bars(context.Background(), req()); err == nil {
		t.Error("no error")
	}
}

func TestSnapshotReportsEachEntryAndNeverLooksAhead(t *testing.T) {
	now := t0.Add(3*time.Hour + 30*time.Minute)
	f := Fixture{Series: map[string][]marketdata.Bar{"EURUSD/h1": {bar(t, 0), bar(t, 1), bar(t, 2), bar(t, 3)}}, Coverage: Coverage{Complete: true}}
	snap := TakeSnapshot(context.Background(), f, []string{"EURUSD", "GBPUSD"}, now)
	if len(snap.Entries) != 4 || !snap.AsOf.Equal(now) {
		t.Fatalf("snapshot = %+v", snap)
	}
	by := map[string]SnapshotEntry{}
	for _, e := range snap.Entries {
		by[e.Instrument+"/"+e.Interval] = e
	}
	h1 := by["EURUSD/h1"]
	// Bar 3 opens at 03:00 but is only available at 04:00, after now.
	if h1.Latest == nil || !h1.Latest.Time.Equal(t0.Add(2*time.Hour)) || !h1.Complete || h1.Fingerprint == "" || h1.Error != "" {
		t.Errorf("EURUSD/h1 = %+v", h1)
	}
	for _, k := range []string{"EURUSD/d1", "GBPUSD/h1", "GBPUSD/d1"} {
		if e := by[k]; e.Latest != nil || e.Error == "" {
			t.Errorf("%s = %+v, want an explicit error and no bar", k, e)
		}
	}
}

func TestBarsJSONIsStableAndOmitsAbsentSeries(t *testing.T) {
	f := Fixture{Series: map[string][]marketdata.Bar{"EURUSD/h1": {bar(t, 0)}}, Coverage: Coverage{Complete: true}, Now: t0}
	got, err := f.Bars(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Fingerprint string
		Bars        []map[string]any
	}
	if err := json.Unmarshal(data, &back); err != nil || len(back.Bars) != 1 || back.Fingerprint != got.Fingerprint {
		t.Fatalf("%s: %v", data, err)
	}
	b := back.Bars[0]
	if _, ok := b["trade"]; ok || b["time"] != "2024-01-02T00:00:00Z" || b["bid"].(map[string]any)["open"] != "1.1" {
		t.Errorf("bar = %v", b)
	}
	snap, _ := json.Marshal(TakeSnapshot(context.Background(), f, []string{"EURUSD"}, t0.Add(2*time.Hour)))
	if !strings.Contains(string(snap), `"latest":{"time":"2024-01-02T00:00:00Z"`) {
		t.Errorf("snapshot = %s", snap)
	}
}
