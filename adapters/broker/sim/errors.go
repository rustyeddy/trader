package sim

import "errors"

// ErrInvalidConfig reports a Deps or AccountConfig value that fails
// NewBroker's validation.
var ErrInvalidConfig = errors.New("sim: invalid config")

// ErrInvalidObservation reports an Observation that fails Advance's
// validation.
var ErrInvalidObservation = errors.New("sim: invalid observation")

// ErrAmbiguousIntrabarOrder reports that an Observation passed to
// Advance would trigger more than one of an account's pending orders
// for the same listing within one bar, under IntrabarRejectAmbiguous
// (ADR-026). OHLC data alone cannot establish which order's trigger
// the market actually reached first, and which order goes first is not
// cosmetic: applying two same-bar fills in different orders can
// produce different resulting positions and realized PnL (for example,
// increase-then-reduce leaves a different average price than
// reduce-then-increase would). None of the conflicting account's
// orders for that listing are filled; every one remains exactly as it
// was.
var ErrAmbiguousIntrabarOrder = errors.New("sim: observation would trigger more than one pending order for this listing within one bar")

// ErrUnsupportedSettlementCurrency reports a fill attempted against a
// listing whose settlement currency (instrument.Spec
// .SettlementCurrency) does not match the account's own currency
// (issue #152, M3-09). Realized/unrealized PnL, cash, and fees are all
// computed and accumulated in a listing's settlement currency; without
// an FX conversion-rate source — which this package deliberately does
// not invent — that arithmetic can only ever succeed when the two
// currencies already agree. This is checked and rejected explicitly,
// before any other part of a fill is built, rather than left to
// surface as a num.ErrCurrencyMismatch deep inside PnL accounting.
var ErrUnsupportedSettlementCurrency = errors.New("sim: listing settlement currency does not match account currency")
