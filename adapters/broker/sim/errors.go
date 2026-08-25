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
