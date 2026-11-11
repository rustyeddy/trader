package sim

import (
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// ModelInfo identifies one configured FillPriceSource, SlippageModel,
// or CommissionModel implementation for later reproducibility records
// — a backtest run manifest, for example (issue #153, M3-10). Name and
// Version identify the model implementation; Config is a canonical,
// deterministic string representation of whatever parameters
// distinguish this specific configured instance from another instance
// of the same Name/Version (for example "pips=1" versus "pips=5") —
// two differently configured instances of the same model must not be
// indistinguishable in a manifest. Config must never contain a secret
// or credential. There is no map[string]any escape hatch here
// deliberately: an implementation is responsible for its own canonical
// (stable, deterministic) formatting, the same discipline this
// package's own determinism guarantees already require throughout.
type ModelInfo struct {
	Name    string
	Version string
	Config  string
}

// SlippageModel adjusts a market-type execution's base price to model
// price impact (issue #153, M3-10). It is consulted only for Market
// and Stop fills — never Limit, which is a price guarantee by
// definition and must never be adjusted — and only when Deps.Slippage
// is configured; a nil Deps.Slippage means no slippage, the
// deterministic exact-price default.
//
// Like FillPriceSource, an implementation must be deterministic and
// must not read a wall clock, global random source, environment, or
// configuration itself: any randomness a future stochastic
// implementation needs must be supplied explicitly at its own
// construction (a seed or injected entropy source), never read from
// ambient state.
type SlippageModel interface {
	// Slippage returns the final executable price for one fill of
	// quantity against listing/side, given price (the base price
	// FillPriceSource or the observation trigger/gap rules already
	// resolved) — not a delta or an offset. The returned price is
	// validated against listing's tick size immediately, before it is
	// used for anything else (including consulting CommissionModel);
	// Slippage does not bypass that check by returning something
	// order.NewFill would later reject.
	Slippage(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (num.Price, error)
	// Info identifies this configured model instance.
	Info() ModelInfo
}

// CommissionModel computes the commission for one fill (issue #153,
// M3-10), consulted from the final price a fill actually executes at
// (after any SlippageModel has already adjusted it) — a percentage- or
// notional-based fee model must see the price that was actually paid,
// not a pre-slippage estimate. A nil Deps.Commission means no
// commission, the deterministic no-fee default.
//
// The same determinism contract as SlippageModel applies: no wall
// clock, global randomness, environment, or configuration reads.
type CommissionModel interface {
	// Commission returns the commission owed for one fill of quantity
	// against listing/side at price, or nil if this fill incurs none.
	// The returned Money's currency is validated the same way any
	// other fill-derived Money is (issue #152's settlement-currency
	// boundary): a commission denominated differently from the
	// account's own currency fails the fill atomically rather than
	// being silently converted or combined.
	Commission(listing instrument.Listing, side order.Side, quantity num.Quantity, price num.Price) (*num.Money, error)
	// Info identifies this configured model instance.
	Info() ModelInfo
}
