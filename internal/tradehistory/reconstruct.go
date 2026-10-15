package tradehistory

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// netMethod names how Net is derived, so no reader assumes spread cost was
// subtracted.
const netMethod = "broker realized P/L + commission + financing + other fees; spread cost already embedded in realized P/L"

type bookKey struct {
	instrument string
	side       order.Side
}

// Reconstruct derives completed trades, open positions and the activities
// that fall outside both from the activities of one stream. The input may be
// in any order and may repeat an identical activity; the output depends only
// on the set of activities.
func Reconstruct(activities []accountactivity.Activity) (Result, error) {
	var res Result
	if len(activities) == 0 {
		return res, nil
	}

	acts, err := normalizeInput(activities)
	if err != nil {
		return res, err
	}

	r := &reconstructor{books: map[bookKey]*cycle{}, stream: acts[0].SyncKey()}
	for _, a := range acts {
		switch a.Kind {
		case accountactivity.KindFill:
			if err := r.fill(a); err != nil {
				return Result{}, fmt.Errorf("fill %s: %w", a.Key(), err)
			}
		case accountactivity.KindFinancing, accountactivity.KindFee:
			r.res.Unattributed = append(r.res.Unattributed, a.Key())
		}
	}
	if err := r.finish(); err != nil {
		return Result{}, err
	}
	return r.res, nil
}

// normalizeInput validates, rejects mixed streams and conflicting repeats,
// drops identical repeats, and puts the activities in the ledger's total
// order.
func normalizeInput(in []accountactivity.Activity) ([]accountactivity.Activity, error) {
	stream := in[0].SyncKey()
	byKey := make(map[accountactivity.Key]accountactivity.Activity, len(in))
	for _, a := range in {
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidActivity, err)
		}
		if a.SyncKey() != stream {
			return nil, fmt.Errorf("%w: %s and %s", ErrMixedStreams, stream, a.SyncKey())
		}
		if prev, ok := byKey[a.Key()]; ok {
			if !prev.SameRecord(a) {
				return nil, fmt.Errorf("%w: %s", ErrConflict, a.Key())
			}
			continue
		}
		// Keep one canonical form of the time so equivalent repeats written
		// in different zones cannot change the output.
		a.OccurredAt = a.OccurredAt.UTC().Round(0)
		byKey[a.Key()] = a
	}
	out := make([]accountactivity.Activity, 0, len(byKey))
	for _, a := range byKey {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return accountactivity.Compare(out[i], out[j]) < 0 })
	return out, nil
}

type reconstructor struct {
	stream accountactivity.SyncKey
	books  map[bookKey]*cycle
	res    Result
}

func opposite(s order.Side) order.Side {
	if s == order.Buy {
		return order.Sell
	}
	return order.Buy
}

// fillParts is how one fill divides between closing an existing position and
// opening a new one.
type fillParts struct {
	closeQ, openQ num.Quantity
	closeEffects  []accountactivity.TradeEffect
	openEffects   []accountactivity.TradeEffect
	// fromEffects is true when broker per-trade units gave the division.
	fromEffects bool
	// byRole is true when the broker's roles alone decided the direction of
	// the whole fill (all effects opened, or all closed/reduced) without
	// per-trade units to size it.
	byRole bool
}

func isClosing(r accountactivity.TradeRole) bool {
	return r == accountactivity.TradeRoleClosed || r == accountactivity.TradeRoleReduced
}

// splitFill decides how much of the fill closes and how much opens, trusting
// the broker over the net position wherever the broker said something:
//
//  1. per-trade units that add up to the fill's size give the exact division
//     (this also handles a hedging account, where a sell can open a short
//     beside an open long);
//  2. failing that, if every effect has the same direction (all opened, or
//     all closed or reduced) the whole fill goes that way, because a role is
//     a broker fact even when its units are missing;
//  3. only with no usable broker information does the net position decide: a
//     fill closes up to the open quantity on the opposite side and opens the
//     rest.
func splitFill(a accountactivity.Activity, closeBook *cycle) (fillParts, error) {
	qty, _ := a.Quantity.Get()
	var p fillParts

	if len(a.TradeEffects) > 0 {
		var closeQ, openQ num.Quantity
		usable := true
		var closing, opening []accountactivity.TradeEffect
		for _, e := range a.TradeEffects {
			if isClosing(e.Role) {
				closing = append(closing, e)
			} else {
				opening = append(opening, e)
			}
			u, ok := e.Units.Get()
			if !ok {
				usable = false
				continue
			}
			var err error
			if isClosing(e.Role) {
				closeQ, err = closeQ.Add(u)
			} else {
				openQ, err = openQ.Add(u)
			}
			if err != nil {
				return p, err
			}
		}
		if usable {
			total, err := closeQ.Add(openQ)
			if err != nil {
				return p, err
			}
			if total.Equal(qty) {
				p.closeQ, p.openQ, p.fromEffects = closeQ, openQ, true
				p.closeEffects, p.openEffects = closing, opening
				return p, nil
			}
		}
		switch {
		case len(closing) > 0 && len(opening) == 0:
			p.closeQ, p.byRole, p.closeEffects = qty, true, closing
			return p, nil
		case len(opening) > 0 && len(closing) == 0:
			p.openQ, p.byRole, p.openEffects = qty, true, opening
			return p, nil
		}
	}

	avail := num.Quantity{}
	if closeBook != nil {
		avail = closeBook.remaining
	}
	p.closeQ = qty
	if avail.Cmp(qty) < 0 {
		p.closeQ = avail
	}
	var err error
	p.openQ, err = qty.Sub(p.closeQ)
	return p, err
}

func (r *reconstructor) fill(a accountactivity.Activity) error {
	side := a.Side
	instKey := a.Instrument.String()
	closeBook := r.books[bookKey{instKey, opposite(side)}]
	parts, err := splitFill(a, closeBook)
	if err != nil {
		return err
	}

	spans := !parts.closeQ.IsZero() && !parts.openQ.IsZero()
	closeLegs := legsFor(a, parts.closeQ, parts.closeEffects, parts, spans)
	openLegs := legsFor(a, parts.openQ, parts.openEffects, parts, spans)

	// Close first, then open: a reversal ends one trade and starts the next.
	var closed *cycle
	var matched, orphanQ num.Quantity
	if !parts.closeQ.IsZero() {
		if closeBook == nil {
			orphanQ = parts.closeQ
			r.res.Orphans = append(r.res.Orphans, Orphan{Source: a.Key(), Quantity: orphanQ})
		} else {
			var err error
			matched, orphanQ, err = closeBook.consume(closeLegs)
			if err != nil {
				return err
			}
			closed = closeBook
			if !orphanQ.IsZero() {
				r.res.Orphans = append(r.res.Orphans, Orphan{Source: a.Key(), Quantity: orphanQ})
			}
		}
	}

	var opened *cycle
	if !parts.openQ.IsZero() {
		bk := bookKey{instKey, side}
		opened = r.books[bk]
		if opened == nil {
			opened = newCycle(r.stream, a, side)
			r.books[bk] = opened
		}
		if err := opened.add(openLegs); err != nil {
			return err
		}
	}

	if err := r.attribute(a, parts, closed, opened, !orphanQ.IsZero(), !matched.IsZero()); err != nil {
		return err
	}

	if closed != nil && closed.remaining.IsZero() {
		t, err := closed.trade(a)
		if err != nil {
			return err
		}
		r.res.Completed = append(r.res.Completed, t)
		delete(r.books, bookKey{instKey, opposite(side)})
	}
	return nil
}

// legsFor builds the legs of one portion of a fill: one per broker trade
// effect when per-trade units divided the fill, otherwise one for the portion
// (carrying the broker trade ID when exactly one effect points that way).
func legsFor(a accountactivity.Activity, qty num.Quantity, effects []accountactivity.TradeEffect, p fillParts, spans bool) []Leg {
	if qty.IsZero() {
		return nil
	}
	if p.fromEffects && len(effects) > 0 {
		legs := make([]Leg, 0, len(effects))
		for _, e := range effects {
			u, _ := e.Units.Get()
			price := e.Price
			if !price.Known() {
				price = a.Price
			}
			legs = append(legs, Leg{Source: a.Key(), At: a.OccurredAt, Quantity: u, Price: price, BrokerTradeID: e.BrokerTradeID})
		}
		return legs
	}
	leg := Leg{Source: a.Key(), At: a.OccurredAt, Quantity: qty, Price: a.Price, Split: spans && !p.byRole}
	if p.byRole && len(effects) == 1 {
		leg.BrokerTradeID = effects[0].BrokerTradeID
		if e := effects[0]; e.Price.Known() {
			leg.Price = e.Price
		}
	}
	return []Leg{leg}
}

// attribute assigns the fill's amounts to the trades it touches.
func (r *reconstructor) attribute(a accountactivity.Activity, p fillParts, closed, opened *cycle, orphaned, closedMatched bool) error {
	if orphaned {
		// Part of this fill closes something the data never showed opening,
		// so its amounts cannot be attributed: a trade it matched, or opened,
		// cannot claim complete totals. A trade it did not touch is unaffected.
		if !closedMatched {
			closed = nil
		}
		for _, c := range []*cycle{closed, opened} {
			if c != nil {
				c.noteSource(a.Key())
				c.markAllUnknown()
			}
		}
		return nil
	}
	if !closedMatched {
		closed = nil
	}
	switch {
	case closed != nil && opened == nil:
		return closed.addFill(a, wholeClosing(a))
	case opened != nil && closed == nil:
		return opened.addFill(a, wholeOpening(a))
	case closed == nil && opened == nil:
		return nil
	}

	// The fill spans two trades. Realized P/L and financing are realized by
	// the closing and belong to the closed trade. The execution costs are
	// incurred by both portions and are divided.
	qty, _ := a.Quantity.Get()
	closeShare := shares{gross: fromValue(a.RealizedPL), fin: fromValue(a.Financing)}
	var openShare shares

	var err error
	if closeShare.spread, openShare.spread, err = divide(a.SpreadCost, p, qty,
		func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] { return e.SpreadCost }); err != nil {
		return err
	}
	if closeShare.fees, openShare.fees, err = divide(a.OtherFees, p, qty,
		func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] { return e.GuaranteedExecutionFee }); err != nil {
		return err
	}
	if closeShare.comm, openShare.comm, err = divide(a.Commission, p, qty, nil); err != nil {
		return err
	}
	if err := closed.addFill(a, closeShare); err != nil {
		return err
	}
	return opened.addFill(a, openShare)
}

// finish emits the open positions in a deterministic order and sorts the
// completed trades.
func (r *reconstructor) finish() error {
	sort.SliceStable(r.res.Completed, func(i, j int) bool {
		a, b := r.res.Completed[i], r.res.Completed[j]
		if !a.ClosedAt.Equal(b.ClosedAt) {
			return a.ClosedAt.Before(b.ClosedAt)
		}
		return a.ID < b.ID
	})
	keys := make([]bookKey, 0, len(r.books))
	for k := range r.books {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].instrument != keys[j].instrument {
			return keys[i].instrument < keys[j].instrument
		}
		return keys[i].side < keys[j].side
	})
	for _, k := range keys {
		r.res.Open = append(r.res.Open, r.books[k].open())
	}
	return nil
}

// quoteCurrency returns the quote currency of a currency pair ID such as
// fx:EUR/USD, the currency prices on that instrument are denominated in.
func quoteCurrency(id instrument.ID) (num.Currency, bool) {
	s := id.String()
	rest, ok := strings.CutPrefix(s, "fx:")
	if !ok {
		return num.Currency{}, false
	}
	_, quote, ok := strings.Cut(rest, "/")
	if !ok {
		return num.Currency{}, false
	}
	c, err := num.ParseCurrency(quote)
	return c, err == nil
}

func sideName(s order.Side) string {
	if s == order.Buy {
		return "long"
	}
	return "short"
}
