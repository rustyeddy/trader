// Package accountreport is the application service behind the account CLI's
// trade views (issue #514, ADR-084). It reads one stream from an
// accountactivity.Store, reconstructs trades from the whole stream with
// tradehistory, and only then applies the requested range, because a position
// cycle may open before the range and close inside it. When the caller asks
// for a benchmark it also measures each fill with executionquality.
//
// It knows nothing about any provider and returns structured values for a
// transport to format.
package accountreport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/executionquality"
	"github.com/rustyeddy/trader/internal/tradehistory"
	"github.com/rustyeddy/trader/num"
)

// Benchmark names the benchmark a caller asks to measure fills against.
// BenchmarkNone means execution quality was not requested, which is a
// different state from requested but unmeasurable.
type Benchmark string

const (
	BenchmarkNone        Benchmark = ""
	BenchmarkBrokerVWAP  Benchmark = "broker-vwap"
	BenchmarkBrokerQuote Benchmark = "broker-quote"
)

// ParseBenchmark reads a --benchmark value.
func ParseBenchmark(s string) (Benchmark, error) {
	switch b := Benchmark(strings.TrimSpace(s)); b {
	case BenchmarkNone, BenchmarkBrokerVWAP, BenchmarkBrokerQuote:
		return b, nil
	default:
		return "", fmt.Errorf("unknown benchmark %q (want %s or %s)", s, BenchmarkBrokerVWAP, BenchmarkBrokerQuote)
	}
}

var (
	// ErrTradeNotFound reports an ID that matches no trade.
	ErrTradeNotFound = errors.New("accountreport: no trade matches")
	// ErrBadRange reports a range whose end is before its start.
	ErrBadRange = errors.New("accountreport: range end is before its start")
)

// AmbiguousError reports an ID prefix that matches several trades. It lists
// them so the caller can pick; the service never chooses one.
type AmbiguousError struct {
	Prefix     string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("accountreport: %q matches %d trades: %s", e.Prefix, len(e.Candidates), strings.Join(e.Candidates, ", "))
}

// Params selects trades from one stream.
type Params struct {
	Store  accountactivity.Store
	Stream accountactivity.SyncKey
	// From is inclusive and To exclusive, applied to a completed trade's
	// close time; a zero bound is unbounded.
	From, To time.Time
	// Benchmark, if set, measures each fill against it.
	Benchmark Benchmark
	// AccountCurrency overrides the currency found in the ledger.
	AccountCurrency num.Currency
}

// Currency is the account currency and where it came from; zero means
// unknown.
type Currency struct {
	Code   num.Currency
	Known  bool
	Source string
}

// FillQuality is one fill measured against the requested benchmark.
type FillQuality struct {
	Source accountactivity.Key
	// Result is nil when the measurement could not be made; Reason says why.
	Result *executionquality.Result
	Reason string
}

// Quality is the execution quality of a trade, present only when a benchmark
// was requested.
type Quality struct {
	Benchmark Benchmark
	Fills     []FillQuality
	// SlippageCost is the sum of the trade's fill costs, known only when every
	// fill was measured with a known cost and no fill is shared with another
	// trade. Reason says why it is unknown.
	SlippageCost accountactivity.Value[num.Money]
	Reason       string
}

// TradeRow is one completed trade and, if requested, its execution quality.
type TradeRow struct {
	Trade   tradehistory.Trade
	Quality *Quality
}

// TradesResponse is the trade view of one stream.
type TradesResponse struct {
	Stream          accountactivity.SyncKey
	From, To        time.Time
	Benchmark       Benchmark
	AccountCurrency Currency
	// Trades are the completed trades closed in the range, ordered by close
	// time then ID.
	Trades []TradeRow
	// Open are the positions still open at the end of the data; the range
	// does not apply to them.
	Open []tradehistory.OpenPosition
	// Orphans and Unattributed are everything reconstruction could not place,
	// reported rather than dropped.
	Orphans      []tradehistory.Orphan
	Unattributed []accountactivity.Key
}

// Trades reconstructs the stream and returns its trades in the range.
func Trades(ctx context.Context, p Params) (TradesResponse, error) {
	l, err := load(ctx, p)
	if err != nil {
		return TradesResponse{}, err
	}
	resp := TradesResponse{
		Stream: p.Stream, From: p.From, To: p.To, Benchmark: p.Benchmark,
		AccountCurrency: l.currency,
		Trades:          []TradeRow{}, Open: l.res.Open, Orphans: l.res.Orphans, Unattributed: l.res.Unattributed,
	}
	if resp.Open == nil {
		resp.Open = []tradehistory.OpenPosition{}
	}
	if resp.Orphans == nil {
		resp.Orphans = []tradehistory.Orphan{}
	}
	if resp.Unattributed == nil {
		resp.Unattributed = []accountactivity.Key{}
	}
	for _, t := range l.res.Completed {
		if !inRange(t.ClosedAt, p.From, p.To) {
			continue
		}
		row := TradeRow{Trade: t}
		if p.Benchmark != BenchmarkNone {
			row.Quality = l.quality(p.Benchmark, t.Sources, legsOf(t))
		}
		resp.Trades = append(resp.Trades, row)
	}
	return resp, nil
}

// Detail is the forensic view of one trade: the trade, the fills it came
// from in full, and their execution quality when requested.
type Detail struct {
	Stream          accountactivity.SyncKey
	AccountCurrency Currency
	Benchmark       Benchmark
	// Completed or Open is set, never both.
	Completed *tradehistory.Trade
	Open      *tradehistory.OpenPosition
	// Fills are the source activities in the order the trade first used them.
	Fills   []accountactivity.Activity
	Quality *Quality
}

// Trade returns one trade by its full ID or a unique prefix of it, whatever
// the range: an ID names a trade. Zero matches is ErrTradeNotFound; several
// is an *AmbiguousError.
func Trade(ctx context.Context, p Params, id string) (Detail, error) {
	if strings.TrimSpace(id) == "" {
		return Detail{}, fmt.Errorf("%w: empty id", ErrTradeNotFound)
	}
	p.From, p.To = time.Time{}, time.Time{}
	l, err := load(ctx, p)
	if err != nil {
		return Detail{}, err
	}

	type hit struct {
		id        string
		completed *tradehistory.Trade
		open      *tradehistory.OpenPosition
	}
	var hits []hit
	for i := range l.res.Completed {
		t := &l.res.Completed[i]
		if t.ID == id {
			hits = []hit{{id: t.ID, completed: t}}
			break
		}
		if strings.HasPrefix(t.ID, id) {
			hits = append(hits, hit{id: t.ID, completed: t})
		}
	}
	if len(hits) != 1 || hits[0].id != id {
		for i := range l.res.Open {
			o := &l.res.Open[i]
			if o.ID == id {
				hits = []hit{{id: o.ID, open: o}}
				break
			}
			if strings.HasPrefix(o.ID, id) {
				hits = append(hits, hit{id: o.ID, open: o})
			}
		}
	}
	switch len(hits) {
	case 0:
		return Detail{}, fmt.Errorf("%w %q", ErrTradeNotFound, id)
	case 1:
	default:
		ids := make([]string, len(hits))
		for i, h := range hits {
			ids[i] = h.id
		}
		sort.Strings(ids)
		return Detail{}, &AmbiguousError{Prefix: id, Candidates: ids}
	}

	h := hits[0]
	d := Detail{Stream: p.Stream, AccountCurrency: l.currency, Benchmark: p.Benchmark, Completed: h.completed, Open: h.open, Fills: []accountactivity.Activity{}}
	var sources []accountactivity.Key
	var legs []tradehistory.Leg
	if h.completed != nil {
		sources, legs = h.completed.Sources, legsOf(*h.completed)
	} else {
		sources = h.open.Sources
		legs = append(append(legs, h.open.Entries...), h.open.Exits...)
	}
	for _, k := range sources {
		if a, ok := l.byKey[k]; ok {
			d.Fills = append(d.Fills, a)
		}
	}
	if p.Benchmark != BenchmarkNone {
		d.Quality = l.quality(p.Benchmark, sources, legs)
	}
	return d, nil
}

func legsOf(t tradehistory.Trade) []tradehistory.Leg {
	return append(append([]tradehistory.Leg{}, t.Entries...), t.Exits...)
}

func inRange(t, from, to time.Time) bool {
	if !from.IsZero() && t.Before(from) {
		return false
	}
	if !to.IsZero() && !t.Before(to) {
		return false
	}
	return true
}

// loaded is a stream read once and reconstructed once.
type loaded struct {
	res      tradehistory.Result
	byKey    map[accountactivity.Key]accountactivity.Activity
	currency Currency
}

func load(ctx context.Context, p Params) (loaded, error) {
	if p.Store == nil {
		return loaded{}, errors.New("accountreport: a store is required")
	}
	if !p.From.IsZero() && !p.To.IsZero() && p.To.Before(p.From) {
		return loaded{}, ErrBadRange
	}
	if _, err := ParseBenchmark(string(p.Benchmark)); err != nil {
		return loaded{}, err
	}
	// The whole stream, not the range: a cycle needs its full history.
	all, err := p.Store.Query(ctx, accountactivity.Query{Sync: p.Stream})
	if err != nil {
		return loaded{}, err
	}
	res, err := tradehistory.Reconstruct(all)
	if err != nil {
		return loaded{}, err
	}
	l := loaded{res: res, byKey: make(map[accountactivity.Key]accountactivity.Activity, len(all))}
	for _, a := range all {
		l.byKey[a.Key()] = a
	}
	l.currency = accountCurrency(all, p.AccountCurrency)
	return l, nil
}

// accountCurrency is the override if given, else the one currency every
// monetary figure in the stream shares, else unknown. The ledger requires
// money in the account home currency, so agreement is expected; disagreement
// or no money at all leaves it unknown rather than guessed.
func accountCurrency(all []accountactivity.Activity, override num.Currency) Currency {
	if override.IsValid() {
		return Currency{Code: override, Known: true, Source: "configured"}
	}
	var found num.Currency
	for _, a := range all {
		for _, v := range []accountactivity.Value[num.Money]{a.RealizedPL, a.SpreadCost, a.Commission, a.Financing, a.OtherFees, a.Cash} {
			m, ok := v.Get()
			if !ok || !m.IsValid() {
				continue
			}
			if !found.IsValid() {
				found = m.Currency()
			} else if !found.Equal(m.Currency()) {
				return Currency{}
			}
		}
	}
	if !found.IsValid() {
		return Currency{}
	}
	return Currency{Code: found, Known: true, Source: "ledger money fields"}
}

// quality measures each fill the trade used. A fill's cost is attributed to
// a trade only if the trade holds all of that fill; a fill shared with
// another trade (a reversal) would otherwise be counted twice.
func (l loaded) quality(b Benchmark, sources []accountactivity.Key, legs []tradehistory.Leg) *Quality {
	q := &Quality{Benchmark: b, Fills: []FillQuality{}}
	held := map[accountactivity.Key]num.Quantity{}
	for _, leg := range legs {
		sum, err := held[leg.Source].Add(leg.Quantity)
		if err == nil {
			held[leg.Source] = sum
		}
	}

	var total num.Money
	haveTotal, allKnown := false, true
	var reasons []string
	for _, k := range sources {
		a, ok := l.byKey[k]
		if !ok || a.Kind != accountactivity.KindFill {
			continue
		}
		fq := FillQuality{Source: k}
		bench, err := benchmarkFor(b, a)
		if err != nil {
			fq.Reason = err.Error()
		} else if r, err := executionquality.Measure(a, &bench, executionquality.Options{AccountCurrency: l.currency.Code}); err != nil {
			fq.Reason = err.Error()
		} else {
			fq.Result = &r
			if len(r.Unresolved) > 0 {
				fq.Reason = strings.Join(r.Unresolved, "; ")
			}
		}
		q.Fills = append(q.Fills, fq)

		whole := false
		if fillQty, ok := a.Quantity.Get(); ok {
			whole = held[k].Equal(fillQty)
		}
		cost := accountactivity.Value[num.Money]{}
		if fq.Result != nil {
			cost = fq.Result.SlippageCost
		}
		m, known := cost.Get()
		switch {
		case !known:
			allKnown = false
			reasons = append(reasons, fmt.Sprintf("fill %s: %s", k, orUnknown(fq.Reason)))
		case !whole:
			allKnown = false
			reasons = append(reasons, fmt.Sprintf("fill %s is shared with another trade", k))
		case !haveTotal:
			total, haveTotal = m, true
		default:
			s, err := total.Add(m)
			if err != nil {
				allKnown = false
				reasons = append(reasons, err.Error())
			} else {
				total = s
			}
		}
	}
	switch {
	case len(q.Fills) == 0:
		q.Reason = "no fills to measure"
	case !allKnown:
		q.Reason = strings.Join(reasons, "; ")
	case haveTotal:
		if v, err := accountactivity.Derived(total, "sum of the trade's fill slippage costs ("+string(b)+")"); err == nil {
			q.SlippageCost = v
		}
	}
	return q
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func benchmarkFor(b Benchmark, a accountactivity.Activity) (executionquality.Benchmark, error) {
	switch b {
	case BenchmarkBrokerVWAP:
		return executionquality.BenchmarkFromFullVWAP(a)
	case BenchmarkBrokerQuote:
		return executionquality.BenchmarkFromQuoteTouch(a)
	}
	return executionquality.Benchmark{}, fmt.Errorf("unsupported benchmark %q", b)
}
