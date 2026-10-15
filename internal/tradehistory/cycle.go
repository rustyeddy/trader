package tradehistory

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

type cstate uint8

const (
	// cNone means the portion carries nothing for this component, as opposed
	// to an unknown amount.
	cNone cstate = iota
	cKnown
	cUnknown
)

// contrib is what one fill contributes to one component of one trade.
type contrib struct {
	state cstate
	m     num.Money
	// apportioned is true when the amount is a units-proportional share
	// Trader computed rather than a broker fact.
	apportioned bool
}

func fromValue(v accountactivity.Value[num.Money]) contrib {
	m, ok := v.Get()
	if !ok {
		return contrib{state: cUnknown}
	}
	return contrib{state: cKnown, m: m}
}

type shares struct{ gross, fin, spread, comm, fees contrib }

func whole(a accountactivity.Activity) shares {
	return shares{
		gross: fromValue(a.RealizedPL), fin: fromValue(a.Financing),
		spread: fromValue(a.SpreadCost), comm: fromValue(a.Commission), fees: fromValue(a.OtherFees),
	}
}

// divide splits one execution-cost component of a fill between the closing
// and opening trades. Broker per-trade values are used when every effect on
// both sides supplies them (reported, exact). Otherwise a known fill-level
// amount is apportioned by units and marked as such. With no defensible basis
// both shares are unknown.
func divide(fv accountactivity.Value[num.Money], p fillParts, qty num.Quantity,
	pick func(accountactivity.TradeEffect) accountactivity.Value[num.Money]) (closeC, openC contrib, err error) {

	if pick != nil && p.fromEffects {
		cs, okc := sumEffects(p.closeEffects, pick)
		os, oko := sumEffects(p.openEffects, pick)
		if okc && oko {
			return contrib{state: cKnown, m: cs}, contrib{state: cKnown, m: os}, nil
		}
	}
	total, ok := fv.Get()
	if !ok {
		return contrib{state: cUnknown}, contrib{state: cUnknown}, nil
	}
	rate, err := p.closeQ.Div(qty)
	if err != nil {
		return closeC, openC, err
	}
	cm, err := total.MulRate(rate)
	if err != nil {
		return closeC, openC, err
	}
	om, err := total.Sub(cm)
	if err != nil {
		return closeC, openC, err
	}
	return contrib{state: cKnown, m: cm, apportioned: true}, contrib{state: cKnown, m: om, apportioned: true}, nil
}

func sumEffects(es []accountactivity.TradeEffect, pick func(accountactivity.TradeEffect) accountactivity.Value[num.Money]) (num.Money, bool) {
	var total num.Money
	for i, e := range es {
		m, ok := pick(e).Get()
		if !ok {
			return num.Money{}, false
		}
		if i == 0 {
			total = m
			continue
		}
		var err error
		if total, err = total.Add(m); err != nil {
			return num.Money{}, false
		}
	}
	return total, len(es) > 0
}

// acc accumulates one component across the fills of one trade.
type acc struct {
	sum         num.Money
	n           int
	unknown     bool
	apportioned []string
}

func (a *acc) add(c contrib, key accountactivity.Key) error {
	switch c.state {
	case cUnknown:
		a.unknown = true
	case cKnown:
		if a.n == 0 {
			a.sum = c.m
		} else {
			var err error
			if a.sum, err = a.sum.Add(c.m); err != nil {
				return err
			}
		}
		a.n++
		if c.apportioned {
			a.apportioned = append(a.apportioned, key.String())
		}
	}
	return nil
}

// value is the Derived total, or unknown when any contributing fill lacked
// the component or nothing contributed. A partial sum is never returned.
func (a *acc) value(label string) accountactivity.Value[num.Money] {
	if a.unknown || a.n == 0 {
		return accountactivity.Value[num.Money]{}
	}
	method := fmt.Sprintf("sum of broker-reported %s over %d fills", label, a.n)
	if len(a.apportioned) > 0 {
		method += "; includes units-proportional shares from fills " + strings.Join(a.apportioned, ", ")
	}
	v, err := accountactivity.Derived(a.sum, method)
	if err != nil {
		return accountactivity.Value[num.Money]{}
	}
	return v
}

// cycle is a position being built: flat, to a position, to flat again.
type cycle struct {
	stream     accountactivity.SyncKey
	id         string
	instrument instrument.ID
	side       order.Side
	openedAt   time.Time

	entries, exits []Leg
	remaining      num.Quantity

	sources []accountactivity.Key
	seen    map[accountactivity.Key]bool

	gross, spread, comm, fin, fees acc
}

func newCycle(stream accountactivity.SyncKey, a accountactivity.Activity, side order.Side) *cycle {
	return &cycle{
		stream: stream, id: a.Key().String() + "#" + sideName(side),
		instrument: a.Instrument, side: side, openedAt: a.OccurredAt,
		seen: map[accountactivity.Key]bool{},
	}
}

func (c *cycle) noteSource(k accountactivity.Key) {
	if !c.seen[k] {
		c.seen[k] = true
		c.sources = append(c.sources, k)
	}
}

func (c *cycle) add(legs []Leg) error {
	for _, l := range legs {
		var err error
		if c.remaining, err = c.remaining.Add(l.Quantity); err != nil {
			return err
		}
		c.entries = append(c.entries, l)
		c.noteSource(l.Source)
	}
	return nil
}

// consume closes legs against the open quantity, in order. Quantity beyond
// what is open comes back as orphan, never silently dropped.
func (c *cycle) consume(legs []Leg) (matched, orphan num.Quantity, err error) {
	for _, l := range legs {
		take := l.Quantity
		if c.remaining.Cmp(take) < 0 {
			take = c.remaining
		}
		if !take.IsZero() {
			x := l
			x.Quantity = take
			c.exits = append(c.exits, x)
			c.noteSource(l.Source)
			if c.remaining, err = c.remaining.Sub(take); err != nil {
				return
			}
			if matched, err = matched.Add(take); err != nil {
				return
			}
		}
		if rest, e := l.Quantity.Sub(take); e != nil {
			return matched, orphan, e
		} else if !rest.IsZero() {
			if orphan, err = orphan.Add(rest); err != nil {
				return
			}
		}
	}
	return
}

func (c *cycle) addFill(a accountactivity.Activity, s shares) error {
	c.noteSource(a.Key())
	k := a.Key()
	for _, x := range []struct {
		dst *acc
		c   contrib
	}{{&c.gross, s.gross}, {&c.fin, s.fin}, {&c.spread, s.spread}, {&c.comm, s.comm}, {&c.fees, s.fees}} {
		if err := x.dst.add(x.c, k); err != nil {
			return err
		}
	}
	return nil
}

func (c *cycle) markAllUnknown() {
	c.gross.unknown, c.fin.unknown, c.spread.unknown, c.comm.unknown, c.fees.unknown = true, true, true, true, true
}

func totalQty(legs []Leg) (num.Quantity, error) {
	var q num.Quantity
	for _, l := range legs {
		var err error
		if q, err = q.Add(l.Quantity); err != nil {
			return q, err
		}
	}
	return q, nil
}

// avgPrice is the weighted average price of legs: each leg's notional
// (price times quantity) is rounded half-even to 8 decimals, summed, and
// divided by the total quantity, rounded half-even. It is unknown when a leg
// has no price or the quote currency is not known.
func avgPrice(legs []Leg, id instrument.ID) accountactivity.Value[num.Price] {
	ccy, ok := quoteCurrency(id)
	if !ok || len(legs) == 0 {
		return accountactivity.Value[num.Price]{}
	}
	var notional num.Money
	var qty num.Quantity
	for i, l := range legs {
		p, ok := l.Price.Get()
		if !ok {
			return accountactivity.Value[num.Price]{}
		}
		n, err := p.MulQuantity(l.Quantity, ccy)
		if err != nil {
			return accountactivity.Value[num.Price]{}
		}
		if i == 0 {
			notional = n
		} else if notional, err = notional.Add(n); err != nil {
			return accountactivity.Value[num.Price]{}
		}
		if qty, err = qty.Add(l.Quantity); err != nil {
			return accountactivity.Value[num.Price]{}
		}
	}
	avg, err := notional.DivQuantity(qty)
	if err != nil {
		return accountactivity.Value[num.Price]{}
	}
	v, err := accountactivity.Derived(avg, fmt.Sprintf(
		"sum(price*quantity)/sum(quantity) over %d legs; each notional and the quotient rounded half-even to 8 decimals", len(legs)))
	if err != nil {
		return accountactivity.Value[num.Price]{}
	}
	return v
}

func (c *cycle) trade(closing accountactivity.Activity) (Trade, error) {
	entryQ, err := totalQty(c.entries)
	if err != nil {
		return Trade{}, err
	}
	exitQ, err := totalQty(c.exits)
	if err != nil {
		return Trade{}, err
	}
	if !entryQ.Equal(exitQ) {
		return Trade{}, fmt.Errorf("trade %s: entry quantity %s != exit quantity %s", c.id, entryQ, exitQ)
	}
	t := Trade{
		ID: c.id, Stream: c.stream, Instrument: c.instrument, Side: c.side,
		OpenedAt: c.openedAt, ClosedAt: closing.OccurredAt,
		Entries: c.entries, Exits: c.exits,
		EntryQuantity: entryQ, ExitQuantity: exitQ,
		AvgEntryPrice: avgPrice(c.entries, c.instrument),
		AvgExitPrice:  avgPrice(c.exits, c.instrument),
		GrossPL:       c.gross.value("realized P/L"),
		SpreadCost:    c.spread.value("half-spread cost"),
		Commission:    c.comm.value("commission"),
		Financing:     c.fin.value("financing"),
		OtherFees:     c.fees.value("other fees"),
		Sources:       c.sources,
	}
	t.Net = net(t)
	return t, nil
}

// net sums the four components Net is made of, only when all are known.
// Spread cost is deliberately not among them: it is already inside GrossPL.
func net(t Trade) accountactivity.Value[num.Money] {
	var total num.Money
	for i, v := range []accountactivity.Value[num.Money]{t.GrossPL, t.Commission, t.Financing, t.OtherFees} {
		m, ok := v.Get()
		if !ok {
			return accountactivity.Value[num.Money]{}
		}
		if i == 0 {
			total = m
			continue
		}
		var err error
		if total, err = total.Add(m); err != nil {
			return accountactivity.Value[num.Money]{}
		}
	}
	v, err := accountactivity.Derived(total, netMethod)
	if err != nil {
		return accountactivity.Value[num.Money]{}
	}
	return v
}

func (c *cycle) open() OpenPosition {
	return OpenPosition{
		ID: c.id, Stream: c.stream, Instrument: c.instrument, Side: c.side, OpenedAt: c.openedAt,
		Entries: c.entries, Exits: c.exits, Remaining: c.remaining,
		AvgEntryPrice: avgPrice(c.entries, c.instrument),
		Sources:       c.sources,
	}
}
