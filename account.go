package trader

import (
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/num"
)

type account struct {
	id       string
	currency num.Currency // the currency of every amount the account holds
	balance  num.Money
	riskPct  num.Rate // fraction of the account risked per trade

	// maxOrderNotional is the largest notional one order may have: an account
	// trading policy, separate from any provider constraint on the instrument.
	maxOrderNotional num.Money

	positions map[string]*position
	trades    []trade // closed positions, in the order they closed

	closedOutAt time.Time // availability of the frame of the latest margin closeout; zero if none
	// closeoutPending is set while a complete closeout has positions left that
	// had no price in the frame; see enforceMargin.
	closeoutPending bool
}

// newAccount opens an account holding balance, whose currency is the
// account's currency.
func newAccount(id string, balance num.Money) *account {
	act := &account{
		id:        id,
		currency:  balance.Currency(),
		balance:   balance,
		riskPct:   rateOnePercent, // default risk per trade
		positions: make(map[string]*position),
	}
	// A balance with no currency makes an account whose every money operation
	// reports num.ErrMissingCurrency; it is not a panic here.
	if limit, err := num.ParseMoney(defaultMaxOrderNotional, balance.Currency()); err == nil {
		act.maxOrderNotional = limit
	}
	return act
}

// equity is what the risk budget is a fraction of: the balance plus the
// unrealized profit and loss of the open positions. It is exact; cents appear
// only when it is printed (ADR-086).
func (act *account) equity() (num.Money, error) {
	unrealized, err := act.unrealizedPnL()
	if err != nil {
		return num.Money{}, err
	}
	return act.balance.Add(unrealized)
}

// unrealizedPnL is the profit or loss of every open position at its latest
// mark. A position is marked at its fill price when it opens, so a mark always
// exists and an unmarked position is never counted as zero.
func (act *account) unrealizedPnL() (num.Money, error) {
	total, err := zeroMoneyOf(act.currency)
	if err != nil {
		return num.Money{}, err
	}
	for _, sym := range act.positionSymbols() {
		pnl, err := act.positions[sym].unrealizedPnL(act.currency)
		if err != nil {
			return num.Money{}, err
		}
		if total, err = total.Add(pnl); err != nil {
			return num.Money{}, err
		}
	}
	return total, nil
}

// usedMargin is the margin held against open positions, valued at their
// latest mark. Margin reserved by orders that are submitted but not yet filled
// is the broker's to know, since it holds the orders; it passes that to size.
func (act *account) usedMargin() (num.Money, error) {
	total, err := zeroMoneyOf(act.currency)
	if err != nil {
		return num.Money{}, err
	}
	for _, sym := range act.positionSymbols() {
		pos := act.positions[sym]
		m, err := marginFor(pos.instrument, pos.units, pos.markPrice, act.currency)
		if err != nil {
			return num.Money{}, err
		}
		if total, err = total.Add(m); err != nil {
			return num.Money{}, err
		}
	}
	return total, nil
}

// marginFor is the margin units of symbol need at price p: (p * units) *
// marginRate. The whole-unit product is exact, so the only rounding is the one
// at the end, on a value of account size (ADR-087). Sizing refuses an unknown
// instrument, so only a bug can reach here with one.
func marginFor(symbol string, units num.Quantity, p num.Price, currency num.Currency) (num.Money, error) {
	inst, ok := instruments[symbol]
	if !ok {
		panic(fmt.Sprintf("margin for %q: %v", symbol, errUnknownInstrument))
	}
	notional, err := p.MulQuantity(units, currency)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin for %s: %w", symbol, err)
	}
	m, err := notional.MulRate(inst.marginRate)
	if err != nil {
		return num.Money{}, fmt.Errorf("margin for %s: %w", symbol, err)
	}
	return m, nil
}

// marginAvailable is what new positions may be margined against: equity less
// the margin already in use. Sizing keeps it separate from equity: risk is
// measured against equity, the margin limit against marginAvailable. It can be
// zero or negative, which leaves no room for a new position.
func (act *account) marginAvailable() (num.Money, error) {
	equity, err := act.equity()
	if err != nil {
		return num.Money{}, err
	}
	used, err := act.usedMargin()
	if err != nil {
		return num.Money{}, err
	}
	return equity.Sub(used)
}

// errUnsupportedAction is returned for actions the account cannot size yet.
// It is a programming gap, not a trading outcome.
var errUnsupportedAction = errors.New("action not supported yet")

// size decides how many units the order trades, or rejects it. The strategy
// says what it wants; the account decides how much.
//
//   - open: sized to the smaller of the risk limit (a stop-out at the stop loses
//     about riskPct of equity; it is planned risk, not a cap) and the margin limit
//     (see sizeOpen), or rejected with errInsufficientMargin or errOrderTooSmall
//     if that is below the instrument's minimum.
//   - close: units stay 0 until the fill, which closes the whole position.
//   - add, reduce and anything else: errUnsupportedAction until their size
//     rules are defined.
func (act *account) size(o *order, reserved num.Money) error {
	inst, ok := instruments[o.instrument]
	if !ok {
		return fmt.Errorf("size %s: %w: %q", o.action, errUnknownInstrument, o.instrument)
	}
	if !inst.quote.Equal(act.currency) {
		return fmt.Errorf("size %s: %w: %s is quoted in %s, the account holds %s",
			o.action, num.ErrCurrencyMismatch, inst.symbol, inst.quote, act.currency)
	}

	switch o.action {
	case open:
		if err := act.sizeOpen(o, inst, reserved); err != nil {
			return err
		}
	case close:
		// whole position
	default:
		return fmt.Errorf("size %s: %w", o.action, errUnsupportedAction)
	}
	o.record(stateSized, o.Time, "")
	return nil
}

// sizeOpen sets o.units for an opening order. order.validate has already checked
// its prices. Two limits apply, and the smaller one wins:
//
//	riskUnits   = (equity * riskPct) / stopDistance
//	marginUnits = (marginAvailable / marginRate) / price
//	marginAvailable = equity - usedMargin - reserved
//
// reserved is the margin the broker holds for this account's submitted opens
// that have not filled yet; without it, orders sized before they fill would
// each see the same margin available.
//
// Each is rounded to 1e-8 of a unit by one division, then the smaller is
// capped at the account's order notional and the instrument's maximum and
// rounded down to its step. The money is scaled by the rate first and divided
// by the price last, so the one rounding falls on a value of account size and
// not on a small per-unit cost (ADR-087).
//
// A rounding can go either way, so the quantity is then checked: its planned
// risk and its required margin are recomputed from the quantized size and, if
// either exceeds its budget, the size drops a step at a time. The check
// compares rounded values, so it holds up to 0.5e-8 of the account currency
// (ADR-087, "Rounding tolerance"). A margin shortfall therefore makes the
// trade smaller instead of refusing it.
//
// Only if the result is below the instrument's minimum is the order rejected:
// with errInsufficientMargin when margin was the limit, and with
// errOrderTooSmall when the risk budget was.
func (act *account) sizeOpen(o *order, inst *instrument, reserved num.Money) error {
	if inst.marginRate.Sign() <= 0 {
		return fmt.Errorf("size open: %w: %s has no margin rate", errInstrumentConfig, inst.symbol)
	}

	equity, err := act.equity()
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	available, err := act.marginAvailable()
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	margin, err := available.Sub(reserved)
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	if sign, err := moneySign(margin); err != nil {
		return fmt.Errorf("size open: %w", err)
	} else if sign <= 0 {
		return reject(o, o.Time, "%w", errInsufficientMargin)
	}

	dist, _, err := priceDiff(o.price, o.stopLoss)
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	riskBudget, err := equity.MulRate(act.riskPct)
	if err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	// A limit too large for a Money or a Quantity does not bind: another limit
	// is smaller. If none fits, the order is refused with the overflow, never
	// wrapped.
	var limits []sizeLimit
	if !inst.maxUnits.IsZero() {
		limits = append(limits, sizeLimit{"instrument", inst.maxUnits})
	}
	for _, l := range []struct {
		name   string
		budget func() (num.Money, error)
		per    num.Price
	}{
		{"risk", func() (num.Money, error) { return riskBudget, nil }, dist},
		{"margin", func() (num.Money, error) { return margin.DivRate(inst.marginRate) }, o.price},
		{"notional", func() (num.Money, error) { return act.maxOrderNotional, nil }, o.price},
	} {
		budget, err := l.budget()
		var q num.Quantity
		if err == nil {
			q, err = budget.DivPrice(l.per)
		}
		if errors.Is(err, num.ErrOverflow) {
			continue
		}
		if err != nil {
			return fmt.Errorf("size open: %s units: %w", l.name, err)
		}
		limits = append(limits, sizeLimit{l.name, q})
	}
	if len(limits) == 0 {
		return fmt.Errorf("size open: every sizing limit exceeds the largest quantity: %w", num.ErrOverflow)
	}
	units, binding := limits[0].units, limits[0].name
	for _, l := range limits[1:] {
		if l.units.Cmp(units) < 0 {
			units, binding = l.units, l.name
		}
	}
	marginLimited := binding == "margin"
	step := inst.unitStep
	if step.IsZero() {
		step = oneUnit
	}
	if units, err = units.RoundDown(step); err != nil {
		return fmt.Errorf("size open: %w", err)
	}
	for !units.IsZero() {
		over, err := act.overBudget(o, inst, units, riskBudget, margin)
		if err != nil {
			return fmt.Errorf("size open: %w", err)
		}
		if !over {
			break
		}
		if units, err = units.Sub(step); err != nil {
			return fmt.Errorf("size open: %w", err)
		}
	}

	least := inst.minUnits
	if least.IsZero() {
		least = oneUnit
	}
	if units.Cmp(least) < 0 {
		if marginLimited {
			return reject(o, o.Time, "%w: margin covers %s units, minimum %s",
				errInsufficientMargin, units, least)
		}
		return reject(o, o.Time, "%w: %s units, minimum %s", errOrderTooSmall, units, least)
	}

	o.units = units
	return nil
}

// sizeLimit is the most units one budget allows.
type sizeLimit struct {
	name  string
	units num.Quantity
}

// overBudget reports whether units, sized for o, plans to lose more than the
// risk budget at its stop, needs more margin than is available, or exceeds the
// order notional limit.
func (act *account) overBudget(o *order, inst *instrument, units num.Quantity, riskBudget, margin num.Money) (bool, error) {
	dist, _, err := priceDiff(o.price, o.stopLoss)
	if err != nil {
		return false, err
	}
	risk, err := dist.MulQuantity(units, act.currency)
	if err != nil {
		return false, err
	}
	need, err := marginFor(inst.symbol, units, o.price, act.currency)
	if err != nil {
		return false, err
	}
	notional, err := o.price.MulQuantity(units, act.currency)
	if err != nil {
		return false, err
	}
	overRisk, err := risk.Cmp(riskBudget)
	if err != nil {
		return false, err
	}
	overMargin, err := need.Cmp(margin)
	if err != nil {
		return false, err
	}
	overNotional, err := notional.Cmp(act.maxOrderNotional)
	if err != nil {
		return false, err
	}
	return overRisk > 0 || overMargin > 0 || overNotional > 0, nil
}

// mark values the open position in b's instrument at the bar's close. The
// broker calls it after the bar's fills, so a position opened at the bar's open
// is valued at its close.
func (a *account) mark(b bar) {
	if pos := a.positions[b.instrument]; pos != nil {
		pos.markPrice = b.c
	}
}

// applyFill applies a fill at price p, at time at, to the account: it validates
// the fill, realizes any profit or loss, then updates the balance and the position
// together, so a failed fill leaves the account as it was. One position per
// instrument (netting): an open creates it, a close removes it and the order takes
// the closed size. Add and reduce wait on the size type.
//
// The broker only reports the execution; it never touches the balance.
func (a *account) applyFill(o *order, p num.Price, at time.Time) error {
	pos := a.positions[o.instrument]
	switch o.action {
	case open:
		if pos != nil {
			return fmt.Errorf("%w: %s is already %s", errPositionExists, o.instrument, pos.direction)
		}
		// Sizing checked the margin when the order was submitted, but a resting
		// order fills later, after equity may have fallen. The instruction is
		// never resized: it fills as submitted or is rejected.
		need, err := marginFor(o.instrument, o.units, p, a.currency)
		if err != nil {
			return err
		}
		avail, err := a.marginAvailable()
		if err != nil {
			return err
		}
		short, err := need.Cmp(avail)
		if err != nil {
			return err
		}
		if short > 0 {
			return rejected("%w: %s needs %s of margin at the fill, %s available",
				errInsufficientMargin, o.instrument, need, avail)
		}
		notional, err := p.MulQuantity(o.units, a.currency)
		if err != nil {
			return err
		}
		tooLarge, err := notional.Cmp(a.maxOrderNotional)
		if err != nil {
			return err
		}
		if tooLarge > 0 {
			return rejected("%w: %s is %s at the fill, the limit is %s",
				errOrderTooLarge, o.instrument, notional, a.maxOrderNotional)
		}
		a.positions[o.instrument] = &position{
			instrument: o.instrument,
			direction:  o.direction,
			units:      o.units,
			entryPrice: p,
			entryTime:  at,
			markPrice:  p,
		}
	case close:
		if pos == nil {
			return rejected("%w: %s", errNoPosition, o.instrument)
		}
		pnl, err := pos.pnlAt(p, a.currency) // realized: closing at the fill
		if err != nil {
			return err
		}
		balance, err := a.balance.Add(pnl)
		if err != nil {
			return err
		}
		a.balance = balance
		o.units = pos.units
		a.trades = append(a.trades, trade{
			instrument: pos.instrument,
			direction:  pos.direction,
			units:      pos.units,
			entryPrice: pos.entryPrice,
			entryTime:  pos.entryTime,
			exitPrice:  p,
			exitTime:   at,
			reason:     o.reason,
			pnl:        pnl,
		})
		delete(a.positions, o.instrument)
	default:
		return fmt.Errorf("fill %s: %w", o.action, errUnsupportedAction)
	}
	return nil
}

// unrealizedPnL is the profit or loss of the position at its latest mark.
func (p *position) unrealizedPnL(currency num.Currency) (num.Money, error) {
	return p.pnlAt(p.markPrice, currency)
}

// pnlAt is the profit or loss of the position valued at exit: realized when
// exit is a fill, unrealized when it is a mark. It is exact: a whole-unit
// quantity times a price difference needs no rounding. Cents appear only when
// it is printed, floored toward a larger loss (ADR-086).
//
// It excludes spread and commission. The quote currency is the account
// currency; account.size refuses an instrument quoted in another.
func (p *position) pnlAt(exit num.Price, currency num.Currency) (num.Money, error) {
	dist, above, err := priceDiff(exit, p.entryPrice)
	if err != nil {
		return num.Money{}, err
	}
	magnitude, err := dist.MulQuantity(p.units, currency)
	if err != nil {
		return num.Money{}, fmt.Errorf("pnl of %s: %w", p.instrument, err)
	}
	if above == (p.direction == long) {
		return magnitude, nil
	}
	return magnitude.Neg()
}
