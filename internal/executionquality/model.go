package executionquality

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// BenchmarkKind says what a benchmark price is a reference for. The kinds are
// not interchangeable, so the kind is part of the result.
type BenchmarkKind string

const (
	// OrderReference is the price Trader requested with the order.
	OrderReference BenchmarkKind = "order_reference"
	// SubmitQuoteMid is the quote mid captured when Trader submitted the
	// order (decision-to-fill, implementation-shortfall style).
	SubmitQuoteMid BenchmarkKind = "submit_quote_mid"
	// TriggerPrice is a stop or limit trigger, where that is the semantic
	// reference.
	TriggerPrice BenchmarkKind = "trigger_price"
	// HistoricalMid is a retrospective mid, usually from bars. It is a weaker
	// claim than a quote, so its bar Interval is required.
	HistoricalMid BenchmarkKind = "historical_mid"
	// BrokerFullVWAP is the broker's price absent guaranteed-price execution.
	BrokerFullVWAP BenchmarkKind = "broker_full_vwap"
	// BrokerQuote is the touch of the broker's own execution quote.
	BrokerQuote BenchmarkKind = "broker_quote"
)

func (k BenchmarkKind) valid() bool {
	switch k {
	case OrderReference, SubmitQuoteMid, TriggerPrice, HistoricalMid, BrokerFullVWAP, BrokerQuote:
		return true
	}
	return false
}

// Benchmark is the reference price a fill is measured against. The caller
// chooses it; Measure never does.
type Benchmark struct {
	Kind BenchmarkKind
	// Price must be positive.
	Price num.Price
	// At is when the reference was taken. Zero means unknown, and the result
	// then reports no benchmark age.
	At time.Time
	// Source says where the reference came from, in words a report can show.
	// Required.
	Source string
	// Interval is the bar length behind a HistoricalMid. Required for it,
	// rejected for every other kind: a bar-derived mid is a bar-level
	// approximation and must say so, never quote-level.
	Interval time.Duration
}

// Validate reports why b cannot be used as a benchmark.
func (b Benchmark) Validate() error {
	switch {
	case !b.Kind.valid():
		return fmt.Errorf("%w: unknown benchmark kind %q", ErrInvalidBenchmark, b.Kind)
	case b.Price.IsZero():
		return fmt.Errorf("%w: benchmark price must be positive", ErrInvalidBenchmark)
	case strings.TrimSpace(b.Source) == "":
		return fmt.Errorf("%w: benchmark requires a source", ErrInvalidBenchmark)
	case b.Kind == HistoricalMid && b.Interval <= 0:
		return fmt.Errorf("%w: a historical mid requires its bar interval", ErrInvalidBenchmark)
	case b.Kind != HistoricalMid && b.Interval != 0:
		return fmt.Errorf("%w: only a historical mid has a bar interval", ErrInvalidBenchmark)
	}
	return nil
}

// Conversion is an explicit conversion of money between two currencies, with
// the provenance a reader needs to trust it. Rate converts one unit of From
// into To.
type Conversion struct {
	Rate   num.Rate
	From   num.Currency
	To     num.Currency
	At     time.Time
	Source string
}

func (c Conversion) validate() error {
	switch {
	case !c.From.IsValid() || !c.To.IsValid():
		return fmt.Errorf("%w: conversion needs both currencies", ErrInvalidConversion)
	case c.Rate.Sign() <= 0:
		return fmt.Errorf("%w: conversion rate must be positive", ErrInvalidConversion)
	case strings.TrimSpace(c.Source) == "":
		return fmt.Errorf("%w: conversion requires a source", ErrInvalidConversion)
	}
	return nil
}

// Options are the account facts a measurement needs.
type Options struct {
	// AccountCurrency is the currency SlippageCost is expressed in.
	// Required.
	AccountCurrency num.Currency
	// Conversion is used only when the instrument's quote currency differs
	// from AccountCurrency. It must run from the quote currency to the
	// account currency.
	Conversion *Conversion
}

// Result is one fill measured against one benchmark. Unknown values are the
// zero Value; Unresolved says why.
type Result struct {
	Source     accountactivity.Key
	Instrument instrument.ID
	Side       order.Side
	// Quantity is the fill size the measurement used.
	Quantity num.Quantity

	// ExecutionPrice carries its method as the Value's source.
	ExecutionPrice accountactivity.Value[num.Price]
	// Benchmark is the reference used, nil when none was supplied.
	Benchmark *Benchmark

	// Slippage is in price units, account-centric: negative is worse for the
	// trader. SlippageCost is the same effect as money in the account
	// currency, with the same sign.
	Slippage     accountactivity.Value[num.Rate]
	SlippageCost accountactivity.Value[num.Money]

	// Conversion is the conversion used for SlippageCost, nil when the quote
	// currency already is the account currency.
	Conversion *Conversion

	// BenchmarkAge is the fill's time minus the benchmark's time. It is
	// negative when the benchmark was taken after the fill. AgeKnown is false
	// when the benchmark carried no time.
	BenchmarkAge time.Duration
	AgeKnown     bool

	// Unresolved lists, in plain words, why anything above is unknown.
	Unresolved []string
}

var (
	// ErrInvalidBenchmark is returned for a benchmark that cannot be used.
	ErrInvalidBenchmark = errors.New("executionquality: invalid benchmark")
	// ErrInvalidConversion is returned for a conversion that cannot be used.
	ErrInvalidConversion = errors.New("executionquality: invalid conversion")
	// ErrNotAFill is returned when the activity is not a fill with a known
	// side and quantity.
	ErrNotAFill = errors.New("executionquality: activity is not a measurable fill")
)
