package trader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

type broker struct {
	id       string
	markets  *catalog // the run's instruments, shared with every account
	accounts map[string]*account
	orders   map[string][]*order // submitted orders awaiting execution, by instrument, for every account
	seq      int                 // last order id issued

	// margin and closeout decide when and what to liquidate. Nil means
	// defaultMarginPolicy and closeWorstLoser.
	margin   marginPolicy
	closeout closeoutPolicy

	// completeCloseout makes a liquidation close every position of the account.
	// By default it is progressive: one position at a time, as the closeout
	// policy chooses, until the margin policy no longer demands more.
	completeCloseout bool

	// clock is the availability of the latest frame the broker advanced
	// through; zero before the first. Every account is valued as of then.
	clock time.Time

	// completed keeps every order that has reached a terminal state (filled,
	// rejected by the broker or cancelled), in the order it happened, for the
	// journal. Only complete appends to it. The broker's own liquidations are in
	// it too; closeouts picks them out by their reason.
	completed []*order

	// amended keeps every amend that was applied, in the order it happened. An
	// amend changes the resting exits of a position rather than executing, so
	// it is not in completed and is not counted as a fill; each exit it changed
	// also carries the change in its own history.
	amended []*order
}

func newBroker(id string, markets *catalog) *broker {
	return &broker{
		id:       id,
		markets:  markets,
		accounts: make(map[string]*account),
		orders:   make(map[string][]*order),
	}
}

func (br *broker) addAccount(id string, balance num.Money) *account {
	act := newAccount(id, balance, br.markets)
	br.accounts[id] = act
	return act
}

// submit has the order's account size it, assigns it an id and queues it to
// await execution. The order is filled (or rejected) on a later bar by onBar;
// submit itself never fills. An expected rejection is recorded on the order
// and returned for the caller to classify.
func (br *broker) submit(ctx context.Context, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	act, ok := br.accounts[o.accountID]
	if !ok {
		return fmt.Errorf("submit to broker %s: %w: no account %q", br.id, errUnknownAccount, o.accountID)
	}

	if o.action == amend {
		return br.amend(ctx, act, o)
	}

	if o.action == open {
		// A margin closeout is not undone by a new open decided on the same
		// frame; later re-entry rules are a future policy. Both times are
		// availabilities, the clock decisions are made on. An open with no
		// decision time counts as decided before any closeout.
		// While a complete closeout is still waiting on a price, every open is
		// refused: the closeout would liquidate it as soon as it filled.
		if act.closeoutPending || !act.closedOutAt.IsZero() && !o.decidedAt.After(act.closedOutAt) {
			return reject(o, o.Time, "%w: account %s at %s",
				errCloseoutGuard, o.accountID, act.closedOutAt.Format(time.DateOnly))
		}
		if br.hasPendingOpen(o.accountID, o.instrument) {
			return reject(o, o.Time, "%w: account %s on %s", errOpenPending, o.accountID, o.instrument)
		}
	}

	reserved, err := br.reservedMargin(act)
	if err != nil {
		return fmt.Errorf("submit to broker %s: %w", br.id, err)
	}
	if err := act.size(o, reserved); err != nil {
		return err
	}

	if o.id == "" {
		o.id = br.nextID()
	}
	slog.InfoContext(ctx, "order submitted",
		"broker", br.id,
		"account", o.accountID,
		"order", o.id,
		"instrument", o.instrument,
		"action", o.action,
		"direction", o.direction,
		"type", o.orderType,
		"units", o.units)
	br.enqueue(o)
	return nil
}

// enqueue marks the order submitted and adds it to the instrument's queue.
func (br *broker) enqueue(o *order) {
	o.record(stateSubmitted, o.Time, "")
	br.orders[o.instrument] = append(br.orders[o.instrument], o)
}

// cancel withdraws the account's waiting order id at time at, on request. The
// order ends cancelled, leaves the queue and releases the margin it reserved.
// An order that is not waiting (it filled, expired or was cancelled already, or
// belongs to another account) is an expected rejection, errNoOrder.
func (br *broker) cancel(ctx context.Context, accountID, orderID string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := br.cancelWhere(at, "cancelled by request", func(o *order) bool {
		return o.id == orderID && o.accountID == accountID
	})
	if n == 0 {
		return rejected("cancel %s on broker %s: %w", orderID, br.id, errNoOrder)
	}
	return nil
}

// amend applies the requested change to the protective stop and take profit of
// the account's open position in o's instrument: the resting exits the broker
// owns. It never loosens protection, because the account sized the position
// from its stop: a long's stop only rises and a short's only falls, a trail
// only gets shorter (or is set on a fixed stop, which keeps its level and
// trails from the next bar), and a target can move anywhere on the winning side
// of the latest price. A new stop must lie on the losing side of the latest
// price, or it would already have triggered. Every change is checked before
// any is made, so a refused amend changes nothing. A refusal is an expected
// rejection, recorded on o. The change is in force from the next bar: bars the
// broker has already processed are not replayed, so an amended stop cannot
// trigger on a bar that opened before the order was decided.
func (br *broker) amend(ctx context.Context, act *account, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m, ok := act.markets.lookup(o.instrument)
	if !ok {
		return fmt.Errorf("amend: %w: %q", errUnknownInstrument, o.instrument)
	}
	if err := m.checkOrderPrices(o); err != nil {
		return fmt.Errorf("amend: %w", err)
	}
	pos := act.positions[o.instrument]
	if pos == nil {
		return reject(o, o.Time, "%w: %s on %s", errNoPosition, o.action, o.instrument)
	}
	exit := func(why orderReason) *order {
		i := slices.IndexFunc(br.orders[o.instrument], func(x *order) bool {
			return x.accountID == o.accountID && x.action == close && x.reason == why
		})
		if i < 0 {
			return nil
		}
		return br.orders[o.instrument][i]
	}
	stop, target := exit(reasonStopLoss), exit(reasonTakeProfit)
	isLong := pos.direction == long
	mark := pos.markPrice

	// beyondMark: p lies where a stop belongs (below the price of a long)
	// when losing, or where a target belongs when not.
	side := func(p num.Price, losing bool) bool {
		if isLong == losing {
			return p.Cmp(mark) < 0
		}
		return p.Cmp(mark) > 0
	}
	if !o.stopLoss.IsZero() {
		switch {
		case stop == nil:
			return reject(o, o.Time, "%w: %s on %s has no stop to move", errNoPosition, o.action, o.instrument)
		case isLong && o.stopLoss.Cmp(stop.price) <= 0, !isLong && o.stopLoss.Cmp(stop.price) >= 0:
			return reject(o, o.Time, "%w: stop %s does not tighten the stop %s", errLoosensStop, o.stopLoss, stop.price)
		case !side(o.stopLoss, true):
			return reject(o, o.Time, "%w: stop %s is not beyond the latest price %s", errStopWrongSide, o.stopLoss, mark)
		}
	}
	if !o.trail.IsZero() {
		switch {
		case stop == nil:
			return reject(o, o.Time, "%w: %s on %s has no stop to trail", errNoPosition, o.action, o.instrument)
		case !stop.trail.IsZero() && o.trail.Cmp(stop.trail) >= 0:
			return reject(o, o.Time, "%w: trail %s does not shorten the trail %s", errLoosensStop, o.trail, stop.trail)
		}
	}
	if !o.takeProfit.IsZero() && !side(o.takeProfit, false) {
		return reject(o, o.Time, "%w: target %s is not beyond the latest price %s", errStopWrongSide, o.takeProfit, mark)
	}

	var changes []string
	if !o.stopLoss.IsZero() {
		changes = append(changes, "stop "+stop.price.String()+" to "+o.stopLoss.String())
		stop.price = o.stopLoss
		stop.record(stateSubmitted, o.Time, "stop amended to "+o.stopLoss.String())
	}
	if !o.trail.IsZero() {
		changes = append(changes, "trail "+stop.trail.String()+" to "+o.trail.String())
		stop.trail = o.trail
		stop.record(stateSubmitted, o.Time, "trail amended to "+o.trail.String())
	}
	if !o.takeProfit.IsZero() {
		if target == nil {
			br.placeExit(&order{accountID: o.accountID, instrument: o.instrument, direction: pos.direction, Time: o.Time},
				limitOrder, reasonTakeProfit, o.takeProfit)
			changes = append(changes, "target set to "+o.takeProfit.String())
		} else {
			changes = append(changes, "target "+target.price.String()+" to "+o.takeProfit.String())
			target.price = o.takeProfit
			target.record(stateSubmitted, o.Time, "target amended to "+o.takeProfit.String())
		}
	}
	if o.id == "" {
		o.id = br.nextID()
	}
	o.record(stateSized, o.Time, "")
	o.record(stateSubmitted, o.Time, "")
	o.record(stateFilled, o.Time, strings.Join(changes, ", "))
	br.amended = append(br.amended, o)
	slog.InfoContext(ctx, "order amended", "broker", br.id, "account", o.accountID,
		"order", o.id, "instrument", o.instrument, "changes", strings.Join(changes, ", "))
	return nil
}

// cancelWhere cancels, at time at, every waiting order that match accepts and
// reports how many. Instruments are visited in a fixed order so the journal is
// replayable.
func (br *broker) cancelWhere(at time.Time, reason string, match func(*order) bool) int {
	n := 0
	for _, instrument := range slices.Sorted(maps.Keys(br.orders)) {
		for _, o := range slices.Clone(br.orders[instrument]) {
			if match(o) {
				o.record(stateCancelled, at, reason)
				br.complete(o)
				n++
			}
		}
	}
	return n
}

// hasPendingOpen reports whether the account has a submitted open waiting on the
// instrument.
func (br *broker) hasPendingOpen(accountID, instrument string) bool {
	return slices.ContainsFunc(br.orders[instrument], func(o *order) bool {
		return o.accountID == accountID && o.action == open
	})
}

// nextID issues the next order id.
func (br *broker) nextID() string {
	br.seq++
	return fmt.Sprintf("%s-%d", br.id, br.seq)
}

// reservedMargin is the margin held by the account's submitted opens that have
// not filled yet, at each order's reference price. Closes reserve nothing.
func (br *broker) reservedMargin(act *account) (num.Money, error) {
	total, err := zeroMoneyOf(act.currency)
	if err != nil {
		return num.Money{}, err
	}
	for _, instrument := range slices.Sorted(maps.Keys(br.orders)) {
		for _, o := range br.orders[instrument] {
			if o.accountID != act.id || o.action != open {
				continue
			}
			m, err := br.markets.marginFor(o.instrument, o.units, o.price, act.currency)
			if err != nil {
				return num.Money{}, err
			}
			if total, err = total.Add(m); err != nil {
				return num.Money{}, err
			}
		}
	}
	return total, nil
}

// phases is how many passes onBar makes over an instrument's queue; see phase.
const phases = 4

// phase is the pass of a bar in which the order is considered: market orders
// first (they fill at the open), then resting entries, then protective stops,
// then take profits. The order between entries is unobservable (one pending
// open per account and instrument); stops before targets is the pessimistic
// choice when a bar reaches both.
func (o *order) phase() int {
	switch {
	case o.orderType == marketOrder:
		return 0
	case o.action == open:
		return 1
	case o.orderType == stopOrder:
		return 2
	}
	return 3
}

// onBar advances the broker by one bar: onFrame with a frame of that bar alone.
func (br *broker) onBar(ctx context.Context, b marketdata.Bar) error {
	return br.onFrame(ctx, frame{availableAt: b.AvailableAt, bars: []marketdata.Bar{b}})
}

// onFrame executes the orders waiting on the frame's instruments for every
// account, then values each account's positions at the bars' closes and
// applies the margin rules once, for the whole frame. The trader calls it
// before the strategy sees any of the frame's bars, so an order cannot fill on
// a bar that opened before the order was decided (see execution).
//
// The open is the one price a bar is known to have printed first, so the
// orders it triggers go first, whatever else the bar later does. Every
// position in the frame is first valued at its bar's open, which is what is
// known when those fills happen; a position without a bar in the frame keeps
// its previous mark. Then the frame's at-open fills run, instrument by
// instrument, and only then the orders that depend on the rest of each range,
// in phases (see phase); an exit placed by an entry filled earlier is live in
// the same stage. Closing marks come last, so no fill is admitted against a
// price that had not happened yet.
//
// Assumption: a bar gives only open, high, low and close, not the path between
// the extremes. After the open we take entries before stops and stops before
// targets, each against the bar's whole range. That is pessimistic for a stop
// on the bar a limit or stop entry fills (the low may have come before the
// entry) and optimistic for a target on that bar (the high may have). See
// todo.org for the options. Across instruments the ranges are taken one after
// another, in instrument order: the path between them is unknown too.
func (br *broker) onFrame(ctx context.Context, f frame) error {
	// A cancelled context stops the frame before any state moves, even when no
	// order is waiting and nothing would otherwise consult it.
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkFrame(f); err != nil {
		return err
	}
	// Before the clock or any state moves: a bar must already carry an engine
	// key (ADR-088), or it would silently miss the queues and positions held
	// under it.
	if err := br.markets.checkFrame(f); err != nil {
		return err
	}
	if f.bars[0].Time.Before(br.clock) {
		return fmt.Errorf("%w: frame at %s opens at %s, before %s, when the previous frame was available",
			errFrameOverlap, f.availableAt.Format(time.RFC3339), f.bars[0].Time.Format(time.RFC3339), br.clock.Format(time.RFC3339))
	}
	br.clock = f.availableAt
	atOpen := make([]marketdata.Bar, len(f.bars))
	for i, b := range f.bars {
		br.expire(b)
		atOpen[i] = openOnly(b)
		br.mark(atOpen[i])
	}
	for _, seen := range [][]marketdata.Bar{atOpen, f.bars} {
		for _, b := range seen {
			for phase := 0; phase < phases; phase++ {
				for _, o := range slices.Clone(br.orders[b.Instrument]) {
					if o.phase() != phase {
						continue
					}
					if err := br.processOrder(ctx, b, o); err != nil {
						return err
					}
				}
			}
		}
	}

	for _, b := range f.bars {
		br.trailStops(b)
		br.mark(b)
	}
	for _, id := range slices.Sorted(maps.Keys(br.accounts)) {
		if err := br.enforceMargin(ctx, f, br.accounts[id]); err != nil {
			return fmt.Errorf("margin check %s: %w", id, err)
		}
	}
	return nil
}

// mark values every account's position in b's instrument at b's close.
func (br *broker) mark(b marketdata.Bar) {
	for _, act := range br.accounts {
		act.mark(b)
	}
}

// errFrameOverlap reports a frame whose bars opened before the previous frame
// was available. The broker supports one provider and one timeframe, whose
// bars line up: every frame then opens at or after the previous frame's
// availability. Overlapping bars (mixed timeframes, providers or sessions)
// would value fills against marks from after the fill; they are deferred until
// there is a use case.
var errFrameOverlap = errors.New("frame overlaps the previous one")

// checkFrame refuses a frame the broker cannot advance through in a defined
// order: it must hold at least one bar, every bar must carry the frame's
// availability (unknown is not zero), the instruments must be distinct and in
// order, and the bars must share an opening time. Ordering the fills of bars
// that opened at different times needs an intrabar time policy, which does not
// exist yet.
func checkFrame(f frame) error {
	if len(f.bars) == 0 {
		return errors.New("frame has no bars")
	}
	if f.availableAt.IsZero() {
		return fmt.Errorf("frame of %s at %s has no availability", f.bars[0].Instrument, f.bars[0].Time.Format(time.RFC3339))
	}
	for i, b := range f.bars {
		if !b.AvailableAt.Equal(f.availableAt) {
			return fmt.Errorf("frame at %s: %s bar is available at %s",
				f.availableAt.Format(time.RFC3339), b.Instrument, b.AvailableAt.Format(time.RFC3339))
		}
		if i == 0 {
			continue
		}
		if prev := f.bars[i-1]; prev.Instrument >= b.Instrument {
			return fmt.Errorf("frame at %s: %s after %s, want distinct instruments in order",
				f.availableAt.Format(time.RFC3339), b.Instrument, prev.Instrument)
		}
		if !b.Time.Equal(f.bars[0].Time) {
			return fmt.Errorf("frame at %s: %s opened at %s, %s at %s; mixed opening times are not supported",
				f.availableAt.Format(time.RFC3339), f.bars[0].Instrument, f.bars[0].Time.Format(time.RFC3339),
				b.Instrument, b.Time.Format(time.RFC3339))
		}
	}
	return nil
}

// expire cancels the orders waiting on b's instrument whose expiry has been
// reached, before the bar can execute them.
func (br *broker) expire(b marketdata.Bar) {
	br.cancelWhere(b.Time, "expired", func(o *order) bool {
		return o.instrument == b.Instrument && !o.expires.IsZero() && !b.Time.Before(o.expires)
	})
}

// enforceMargin applies the margin rules to one account after frame f. A
// warning is logged. Liquidation cancels the account's unfilled opens, then
// closes positions one at a time, as the closeout policy chooses. A progressive
// closeout stops as soon as the margin policy is satisfied; a complete one
// closes everything.
//
// The rules judge every position at its latest mark, which is valuation. A
// close is execution, so it needs a price from this frame: when the policy
// picks a position whose instrument has no bar in f, the closeout stops there,
// leaving the account exposed until a later frame brings that instrument a
// fresh bar and the rules run again. It is never filled at the old mark as
// though that were a current quote.
//
// closedOutAt is the frame's availability, the clock decisions are made on, so
// the guard in submit refuses every open decided on this frame, whichever of
// its bars produced it.
//
// A complete closeout that defers a position stays pending on the account: it
// resumes on every later frame, even when the margin policy is satisfied by
// then, until no position is left. A progressive closeout is judged afresh on
// each frame.
func (br *broker) enforceMargin(ctx context.Context, f frame, act *account) error {
	at := f.bars[0].Time // the frame's bars share an opening time (checkFrame)
	policy, pick := br.margin, br.closeout
	if policy == nil {
		policy = defaultMarginPolicy
	}
	if pick == nil {
		pick = closeWorstLoser
	}

	status, err := policy(act)
	if err != nil {
		return err
	}
	if status == marginWarning {
		slog.Warn("margin warning", "broker", br.id, "account", act.id, "time", at)
	}
	switch {
	case status == marginLiquidate:
		slog.Warn("margin closeout", "broker", br.id, "account", act.id, "time", at)
		act.closedOutAt = f.availableAt
		br.cancelWhere(at, "margin closeout", func(o *order) bool {
			return o.accountID == act.id && o.action == open
		})
	case act.closeoutPending:
		slog.Warn("margin closeout resumed", "broker", br.id, "account", act.id, "time", at)
	default:
		return nil
	}
	for status == marginLiquidate || br.completeCloseout {
		pos, err := pick(act)
		if err != nil {
			return err
		}
		if pos == nil {
			act.closeoutPending = false
			return nil // nothing left to close
		}
		i := slices.IndexFunc(f.bars, func(b marketdata.Bar) bool { return b.Instrument == pos.instrument })
		if i < 0 {
			slog.Warn("margin closeout deferred: no current price",
				"broker", br.id, "account", act.id, "instrument", pos.instrument, "time", at)
			act.closeoutPending = br.completeCloseout
			return nil
		}
		if err := br.closePosition(f.bars[i], act, pos, reasonMarginCloseout, pos.markPrice); err != nil {
			return err
		}
		if status, err = policy(act); err != nil {
			return err
		}
	}
	return nil
}

// placeExits queues the protective stop and the take profit, when the filled
// open o carries them, as resting closes the broker owns.
func (br *broker) placeExits(o *order) {
	if !o.stopLoss.IsZero() {
		br.placeExit(o, stopOrder, reasonStopLoss, o.stopLoss).trail = o.trail
	}
	if !o.takeProfit.IsZero() {
		br.placeExit(o, limitOrder, reasonTakeProfit, o.takeProfit)
	}
}

func (br *broker) placeExit(o *order, typ orderType, why orderReason, p num.Price) *order {
	x := &order{
		id:         br.nextID(),
		accountID:  o.accountID,
		instrument: o.instrument,
		direction:  o.direction, // the direction of the position it closes
		action:     close,
		orderType:  typ,
		reason:     why,
		price:      p,
		Time:       o.Time,
	}
	x.record(stateRequested, x.Time, "")
	br.enqueue(x)
	return x
}

// trailStops moves the trailing stops waiting on b's instrument toward profit:
// a long's stop up to the bar's high less the trail, a short's down to the low
// plus it, never back. It runs after the bar's executions, so a stop is always
// checked at its level from before the bar and a bar can never both raise a
// stop and hit the raised level: the path within the bar is unknown, and this
// takes no view on it. Each move is an event in the order's history.
func (br *broker) trailStops(b marketdata.Bar) {
	for _, o := range br.orders[b.Instrument] {
		if o.reason != reasonStopLoss || o.trail.IsZero() {
			continue
		}
		moved := o.price
		if o.direction == long {
			// The high less the trail, when that is above the stop (it can also
			// be below zero, which is below any stop).
			if h := b.OHLC().High; h.Cmp(o.trail) > 0 {
				if level, err := h.Sub(o.trail); err == nil {
					moved = num.Max(o.price, level)
				}
			}
		} else if level, err := b.OHLC().Low.Add(o.trail); err == nil {
			moved = num.Min(o.price, level)
		}
		if !moved.Equal(o.price) {
			o.price = moved
			o.record(stateSubmitted, b.Time, "trailed to "+moved.String())
		}
	}
}

// closePosition closes pos with a market order the broker makes itself, for the
// given reason, filled at once on this bar at p. A closeout that does not fill
// is an error: the margin rules depend on it.
func (br *broker) closePosition(b marketdata.Bar, act *account, pos *position, why orderReason, p num.Price) error {
	o := &order{
		id:         br.nextID(),
		accountID:  act.id,
		instrument: pos.instrument,
		action:     close,
		orderType:  marketOrder,
		reason:     why,
		price:      p,
		Time:       b.Time,
	}
	o.record(stateRequested, b.Time, "")
	o.record(stateSubmitted, b.Time, "")
	err := br.fill(o, p, b.Time)
	if err == nil || isRejection(err) {
		br.complete(o) // a rejected closeout is terminal too, and kept
	}
	if err != nil {
		return fmt.Errorf("%s %s: %v", why, pos.instrument, err) // %v: never an expected outcome here
	}
	return nil
}

// closeouts is the completed orders the broker made itself to liquidate
// positions, including any that were rejected.
func (br *broker) closeouts() []*order {
	var out []*order
	for _, o := range br.completed {
		if o.reason == reasonMarginCloseout {
			out = append(out, o)
		}
	}
	return out
}

// complete is the one place an order reaches its end: it leaves the queue and
// joins completed, in the order the transitions actually happened. A filled
// close ends the position, so the broker's resting exits for it go with it;
// when one exit fills the other is cancelled here (one cancels the other).
func (br *broker) complete(o *order) {
	br.completed = append(br.completed, o)
	br.orders[o.instrument] = slices.DeleteFunc(br.orders[o.instrument], func(q *order) bool { return q == o })
	if o.action == close && o.state == stateFilled {
		br.cancelWhere(o.Time, "position closed", func(x *order) bool {
			return x.accountID == o.accountID && x.instrument == o.instrument &&
				x.action == close && x.reason != reasonStrategy
		})
	}
}

// processOrder gives a submitted order its chance to execute on bar b.
func (br *broker) processOrder(ctx context.Context, b marketdata.Bar, o *order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, ok := o.execution(b)
	if !ok {
		return nil
	}
	if err := br.fill(o, p, b.Time); err != nil && !isRejection(err) {
		return err
	}
	br.complete(o) // filled, or rejected by the account
	return nil
}

// execution reports whether the order executes on bar b and at what price.
//
// A market order fills at the open. A resting order waits for the bar to trade
// through its price and fills there, or at the open when the bar gaps past it
// (nothing traded at the price). A strategy's order never fills on the bar
// that produced it, nor on any bar that opened before the order was decided,
// on any instrument; the broker's own exits may fill on the bar that placed
// them, but never on an earlier one.
//
// Which side of the bar triggers a resting order follows from what it does: a
// buy limit and a sell stop wait for the price to fall, a sell limit and a buy
// stop for it to rise. Opening a long buys, closing one sells.
func (o *order) execution(b marketdata.Bar) (num.Price, bool) {
	if o.state != stateSubmitted || o.instrument != b.Instrument {
		return num.Price{}, false
	}
	if o.reason == reasonStrategy && (!b.Time.After(o.Time) || b.Time.Before(o.decidedAt)) || b.Time.Before(o.Time) {
		return num.Price{}, false
	}
	px := b.OHLC()
	switch o.orderType {
	case marketOrder:
		return px.Open, true
	case limitOrder, stopOrder:
		// Resting orders use the trigger calculation below.
	default:
		return num.Price{}, false
	}
	buys := (o.action == open) == (o.direction == long)
	below := buys == (o.orderType == limitOrder)
	switch {
	case below && px.Low.Cmp(o.price) <= 0:
		return num.Min(o.price, px.Open), true
	case !below && px.High.Cmp(o.price) >= 0:
		return num.Max(o.price, px.Open), true
	}
	return num.Price{}, false
}

// fill reports the execution at p, at time at, to the order's account, which validates it
// and updates its balance and positions together. A fill that cannot apply (a
// close with no position, too little margin now) is an expected rejection,
// recorded on the order and returned; anything else is a failure. A filled
// open places its exits.
func (br *broker) fill(o *order, p num.Price, at time.Time) error {
	act, ok := br.accounts[o.accountID]
	if !ok {
		return fmt.Errorf("fill order %s: %w: %s", o.id, errUnknownAccount, o.accountID)
	}
	if err := act.applyFill(o, p, at); err != nil {
		if isRejection(err) {
			return reject(o, at, "%w", err)
		}
		return fmt.Errorf("fill order %s: %w", o.id, err)
	}
	o.fillPrice = p
	o.record(stateFilled, at, "")
	if o.action == open {
		br.placeExits(o)
	}
	return nil
}

// openOnly returns b as known at its open: each price series the bar has (bid,
// ask, mid, trade) collapsed independently to its own opening price, since all
// that has happened so far is the open. A series the bar lacks stays absent,
// nothing is synthesized, and a valid bar stays valid: the mid of the collapsed
// bid and ask is still the collapsed mid, and an ask that was not below the bid at
// the open is not below it now.
func openOnly(b marketdata.Bar) marketdata.Bar {
	for _, s := range []*marketdata.OHLC{&b.Bid, &b.Ask, &b.Mid, &b.Trade} {
		if !s.IsZero() {
			o := s.Open
			*s = marketdata.OHLC{Open: o, High: o, Low: o, Close: o}
		}
	}
	return b
}
