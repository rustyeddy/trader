package sdk

import (
	"log/slog"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Descriptor identifies one strategy and states what data it needs —
// sdk's own counterpart to strategy.Descriptor, carried
// verbatim in Handshake so the host can answer its own Describe()
// with no further RPC round trip (ADR-062).
type Descriptor struct {
	// Name identifies this strategy, for example "ema_cross".
	Name string
	// Version distinguishes revisions of the same strategy.
	Version string
	// Requirements states the market data this strategy needs before
	// its first OnBar call.
	Requirements []DataRequirement
}

// DataRequirement names one instrument/interval this strategy needs
// bars for, and how many bars of warm-up history it needs first —
// sdk's own counterpart to strategy.DataRequirement.
type DataRequirement struct {
	Instrument instrument.ID
	Interval   marketdata.Interval
	WarmupBars int
}

// BarEvent is one instrument's completed bar, the trigger for OnBar —
// sdk's own counterpart to strategy.BarEvent.
type BarEvent struct {
	Instrument instrument.ID
	Interval   marketdata.Interval
	Bar        marketdata.Bar
}

// FillEvent is one execution report delivered to a strategy
// implementing FillHandler — sdk's own counterpart to
// strategy.FillEvent, carrying exactly the minimal slice
// FillEvent.proto documents (never a reconstructed order.Fill: the
// wire message itself omits FillID/BrokerOrderID/BrokerFillID/
// Timestamp/Commission, so there is nothing to reconstruct them
// from). OrderID, CorrelationID, and CausationID are each that
// identifier's own canonical text form, carried as plain strings — a
// guest never parses or re-mints them, only observes and correlates
// by equality.
type FillEvent struct {
	OrderID       string
	Instrument    instrument.ID
	Side          order.Side
	Price         num.Price
	Quantity      num.Quantity
	CorrelationID string
	CausationID   string
}

// AccountSnapshot is the deliberately minimal account/view slice v1
// carries inline with every BarEvent/FillEvent — sdk's own
// counterpart to the read-only fields an in-process strategy would
// read via View.Account(). See strategy.proto's own AccountSnapshot
// doc comment for exactly why it is this narrow.
type AccountSnapshot struct {
	AccountID string
	Currency  string
	AsOf      time.Time
	Positions []PositionSnapshot
}

// PositionSnapshot is one open position — sdk's own
// counterpart to the fields of order.Position current in-process
// strategies actually read (instrument, side, and average price;
// never quantity or the full Listing — see strategy.proto's own
// PositionSnapshot doc comment).
type PositionSnapshot struct {
	Instrument instrument.ID
	Side       order.PositionSide
	// AvgPrice is nil exactly when Side is order.Flat, mirroring
	// order.Position's own invariant.
	AvgPrice *num.Price
}

// View is the read-only market/portfolio state OnBar/OnFill may
// consult — sdk's own counterpart to strategy.View, plus
// strategy.History's own capability folded in as a required method
// rather than a separately negotiated one: v1 makes GetHistoryBars
// always available, scoped to the guest's own declared
// DataRequirements, not a negotiated capability (strategy.proto's own
// GetHistoryBarsRequest doc comment).
type View interface {
	// Account returns the current, authoritative account snapshot.
	Account() AccountSnapshot

	// HistoryBars returns up to n of the most-recently-closed bars for
	// (instID, interval), strictly before the current callback's own
	// timestamp, oldest-first — the same ordering
	// strategy.History.HistoryBars' own in-process contract documents,
	// but with an explicit third result strategy.History does not
	// have: ok == false alone means exactly "not available" — either
	// (instID, interval) was never declared as one of this strategy's
	// own Descriptor.Requirements, or this View was built for an
	// OnFill callback, which v1 never scopes history to at all — while
	// a non-nil err reports a genuine transport/RPC failure calling
	// the host, distinctly (a dead host must never look identical to
	// an ordinary undeclared-requirement response). n <= 0 is not an
	// error: it reports an empty, non-nil slice with ok == true when
	// the requirement is otherwise declared, mirroring
	// backtest.Scheduler's own in-process History implementation
	// exactly.
	HistoryBars(instID instrument.ID, interval marketdata.Interval, n int) (bars []marketdata.Bar, ok bool, err error)
}

// Environment is Start's own injected-capability bundle — sdk's
// own counterpart to strategy.Environment, deliberately smaller: no
// Intents or Journal field, since a guest never mints a canonical
// order.Intent or writes a journal.Record directly (see the package
// doc comment).
type Environment struct {
	// Clock reflects the host's own injected clock — seeded from
	// SessionStart's own start_time and advanced to each BarEvent's
	// own timestamp as it arrives — never this process's local wall
	// clock (see the package doc comment's own "clock ownership"
	// section).
	Clock clock.Clock

	// RunID is the host's own run identifier, in its canonical text
	// form — carried as a plain string since a guest never parses or
	// re-mints it, only observes it (for example to include in its
	// own diagnostic logging).
	RunID string

	// Logger receives this strategy's own structured records, if any.
	// Never nil: Serve injects logging.Discard() equivalent when the
	// caller supplied none.
	Logger *slog.Logger
}
