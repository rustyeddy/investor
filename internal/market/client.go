package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

const (
	maxPages     = 1000
	maxBody      = 32 << 20
	defaultLimit = 10000
)

// APIError is an error body from Trader's API.
type APIError struct {
	Status   int
	Code     string
	Message  string
	Coverage *Coverage
}

func (e *APIError) Error() string {
	return fmt.Sprintf("trader: %d %s: %s", e.Status, e.Code, e.Message)
}

// Is maps Trader's codes onto this package's sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNoData:
		return e.Code == "no_data"
	case ErrIncomplete:
		return e.Code == "incomplete"
	case ErrUnauthorized:
		return e.Code == "unauthorized"
	}
	return false
}

// Client reads bars from Trader's REST API (GET /v1/marketdata/bars). Trader is
// the only provider; Investor never talks to a broker or reads Trader's files.
type Client struct {
	BaseURL string       // e.g. http://127.0.0.1:8080
	Token   string       // Bearer token, if Trader requires one.
	HTTP    *http.Client // Defaults to a client with a 30s timeout.
	Now     func() time.Time
}

type wireOHLC struct {
	Open  num.Price `json:"open"`
	High  num.Price `json:"high"`
	Low   num.Price `json:"low"`
	Close num.Price `json:"close"`
}

func wireOf(o marketdata.OHLC) *wireOHLC {
	if o.IsZero() {
		return nil
	}
	return &wireOHLC{o.Open, o.High, o.Low, o.Close}
}

// encodeBar is the JSON form of a bar, shared with Trader's wire format: exact
// decimal strings, and an absent price series is left out.
func encodeBar(b marketdata.Bar) wireBar {
	return wireBar{Time: b.Time.UTC(), AvailableAt: b.AvailableAt.UTC(), Volume: b.Volume,
		Bid: wireOf(b.Bid), Ask: wireOf(b.Ask), Mid: wireOf(b.Mid), Trade: wireOf(b.Trade)}
}

func (w *wireOHLC) ohlc() marketdata.OHLC {
	if w == nil {
		return marketdata.OHLC{}
	}
	return marketdata.OHLC{Open: w.Open, High: w.High, Low: w.Low, Close: w.Close}
}

type wireBar struct {
	Time        time.Time `json:"time"`
	AvailableAt time.Time `json:"available_at"`
	Volume      int64     `json:"volume"`
	Bid         *wireOHLC `json:"bid,omitempty"`
	Ask         *wireOHLC `json:"ask,omitempty"`
	Mid         *wireOHLC `json:"mid,omitempty"`
	Trade       *wireOHLC `json:"trade,omitempty"`
}

type wirePage struct {
	Source     string     `json:"source"`
	Instrument string     `json:"instrument"`
	Bars       []wireBar  `json:"bars"`
	NextCursor string     `json:"next_cursor"`
	Coverage   *Coverage  `json:"coverage"`
	Error      *wireError `json:"error"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Bars implements Source: it follows next_cursor to the end, validates every
// bar, and merges the pages' coverage (the first page describes the whole range).
func (c *Client) Bars(ctx context.Context, r Request) (Bars, error) {
	if err := r.Validate(); err != nil {
		return Bars{}, err
	}
	q := url.Values{
		"instrument": {strings.ToUpper(r.Instrument)}, "interval": {r.Interval},
		"from": {r.From.UTC().Format(time.RFC3339)}, "to": {r.To.UTC().Format(time.RFC3339)},
		"limit": {fmt.Sprint(defaultLimit)},
	}
	if !r.AsOf.IsZero() {
		q.Set("as_of", r.AsOf.UTC().Format(time.RFC3339))
	}
	if r.Strict {
		q.Set("partial", "false")
	}
	var (
		bars   []marketdata.Bar
		cov    Coverage
		source string
	)
	for page := 0; ; page++ {
		if page >= maxPages {
			return Bars{}, fmt.Errorf("%w: more than %d pages", ErrInvalid, maxPages)
		}
		p, err := c.get(ctx, q)
		if err != nil {
			return Bars{}, err
		}
		if p.Coverage == nil {
			return Bars{}, fmt.Errorf("%w: response has no coverage", ErrInvalid)
		}
		if page == 0 {
			cov, source = *p.Coverage, p.Source
		} else {
			cov = mergeCoverage(cov, *p.Coverage)
		}
		for _, w := range p.Bars {
			b := marketdata.Bar{
				Instrument: strings.ToUpper(r.Instrument), Time: w.Time, AvailableAt: w.AvailableAt, Volume: w.Volume,
				Bid: w.Bid.ohlc(), Ask: w.Ask.ohlc(), Mid: w.Mid.ohlc(), Trade: w.Trade.ohlc(),
			}
			bars = append(bars, b)
		}
		if p.NextCursor == "" {
			break
		}
		if p.NextCursor == q.Get("cursor") {
			return Bars{}, fmt.Errorf("%w: cursor did not advance", ErrInvalid)
		}
		q.Set("cursor", p.NextCursor)
	}
	if len(bars) == 0 {
		return Bars{}, fmt.Errorf("%w: no bars in a successful response", ErrNoData)
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return finish(r, source, bars, cov, now())
}

// mergeCoverage keeps the first page's verdict and adds the gaps and months
// that later pages report, each of which appears on exactly one page.
func mergeCoverage(a, b Coverage) Coverage {
	for _, m := range b.Months {
		dup := false
		for _, x := range a.Months {
			dup = dup || (x.Year == m.Year && x.Month == m.Month)
		}
		if !dup {
			a.Months = append(a.Months, m)
		}
	}
	a.Gaps = append(a.Gaps, b.Gaps...)
	return a
}

func (c *Client) get(ctx context.Context, q url.Values) (wirePage, error) {
	var p wirePage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/v1/marketdata/bars?"+q.Encode(), nil)
	if err != nil {
		return p, err
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return p, fmt.Errorf("trader: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return p, fmt.Errorf("trader: reading response: %w", err)
	}
	if len(body) > maxBody {
		return p, fmt.Errorf("%w: response larger than %d bytes", ErrInvalid, maxBody)
	}
	if err := json.Unmarshal(body, &p); err != nil && resp.StatusCode == http.StatusOK {
		return p, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if resp.StatusCode != http.StatusOK {
		e := &APIError{Status: resp.StatusCode, Code: "unknown", Message: http.StatusText(resp.StatusCode), Coverage: p.Coverage}
		if p.Error != nil {
			e.Code, e.Message = p.Error.Code, p.Error.Message
		}
		return p, e
	}
	return p, nil
}
