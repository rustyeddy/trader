package backtest

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// ErrInvalidMetrics marks a MetricsParams that fails NewMetrics'
// validation.
var ErrInvalidMetrics = errors.New("backtest: invalid metrics params")

// EquityPoint is one authoritative, mark-to-market account-equity
// observation at a point in simulated time (issue #219, M5-11).
// Scheduler appends one per batch (see its own doc comment for
// exactly when), and Runner prepends the run's own starting
// observation, so a caller never has to guess whether "before the
// first bar" is represented.
type EquityPoint struct {
	Timestamp time.Time
	Equity    num.Money
}

// InstrumentMetrics is one Trade-derived instrument's own slice of the
// aggregate closed-trade figures — GrossPnL/Costs/NetPnL here always
// sum back to the corresponding aggregate Metrics accessor across every
// InstrumentMetrics entry, since both are computed from the exact same
// Trades population.
type InstrumentMetrics struct {
	InstrumentID instrument.ID
	Provider     string
	Venue        string

	Count, Wins, Losses     int
	GrossPnL, Costs, NetPnL num.Money
}

// SideMetrics is one Trade-derived holding direction's (Long or Short)
// own slice of the aggregate closed-trade figures, computed the same
// way InstrumentMetrics is — GrossPnL/Costs/NetPnL here always sum
// back to the corresponding aggregate Metrics accessor across the (at
// most two) SideMetrics entries, since both are computed from the
// exact same Trades population. Side is never order.Flat: a Trade with
// no direction has nothing to report (order.NewTrade's own invariant).
type SideMetrics struct {
	Side order.PositionSide

	Count, Wins, Losses     int
	GrossPnL, Costs, NetPnL num.Money
}

// MetricsParams is NewMetrics' input. Every value here is an
// authoritative observation Runner/Scheduler already collected —
// Metrics itself performs no snapshotting, no event-stream reading,
// and no trade derivation; it only validates and computes (issue
// #219, M5-11).
type MetricsParams struct {
	// StartingCapital and FinalEquity are the run's own authoritative
	// account-level bookends (Manifest.StartingCapital() and
	// Result.Account.Equity()).
	StartingCapital num.Money
	FinalEquity     num.Money
	// EquityCurve is the run's own authoritative, mark-to-market
	// observation series (Scheduler.EquityCurve(), prefixed by
	// Runner with the run's starting observation). NetReturn and
	// MaxDrawdown never reconstruct this from Trades.
	EquityCurve []EquityPoint
	// Trades are the run's fully closed round trips (Result.Trades) —
	// every entry must have ClosedAt set and valid RealizedPnL/Costs;
	// NewMetrics rejects any that don't, since this population is only
	// ever meant to be closed round trips, not a caller-supplied mix.
	// Every closed-trade statistic below — GrossPnL, ClosedTradeCosts,
	// NetPnL, win/loss counts, expectancy, profit factor, and the
	// per-instrument breakdown — is computed from this population
	// only. Still-open positions have no final win/loss outcome yet,
	// so they are deliberately not accepted here at all; account-level
	// figures (FinalEquity, NetReturn) already reflect their
	// unrealized contribution through the authoritative EquityCurve/
	// FinalEquity instead.
	Trades []order.Trade
	// AccountFees is the run's authoritative, account-level cumulative
	// fee total (Result.Account.Fees()) — the run-level cost figure,
	// as opposed to ClosedTradeCosts' closed-trades-only one. It is
	// not required to reconcile against ClosedTradeCosts when
	// OpenTrades remain, since it also includes their already-
	// incurred entry costs.
	AccountFees num.Money
}

// Metrics is the transport-neutral backtest result and performance-
// metrics model (issue #219, M5-11; ADR-035's "Metrics versus
// rendering" section): it lives in backtest, not report, and carries
// no CLI/report-shaped types. Construct it with NewMetrics.
type Metrics struct {
	startingCapital num.Money
	finalEquity     num.Money
	netReturn       num.Rate
	maxDrawdown     num.Rate
	equityCurve     []EquityPoint

	tradeCount                         int
	wins, losses                       int
	winRate                            *num.Rate
	averageWin, averageLoss            *num.Money
	grossPnL, closedTradeCosts, netPnL num.Money
	accountFees                        num.Money
	expectancy                         *num.Money
	profitFactor                       *num.Rate

	perInstrument []InstrumentMetrics
	bySide        []SideMetrics
}

// NewMetrics validates params and computes Metrics. StartingCapital
// and FinalEquity must be valid Money sharing one currency; every
// EquityCurve point and every Trade's RealizedPnL/Costs must be valid
// Money in that same currency (ADR-035's v0 single-account/single-
// currency scope — see the package doc comment for why this is a
// validated precondition, not a case silently converted or skipped).
// EquityCurve timestamps must be non-decreasing. StartingCapital must
// be positive: a real account is never opened at zero or negative
// equity, so a NetReturn denominator of zero is rejected here rather
// than left as an undefined field-access edge case.
func NewMetrics(params MetricsParams) (Metrics, error) {
	currency := params.StartingCapital.Currency()
	if !params.StartingCapital.IsValid() {
		return Metrics{}, fmt.Errorf("%w: starting capital must be valid money", ErrInvalidMetrics)
	}
	if sign, err := params.StartingCapital.Cmp(zeroMoneyMust(currency)); err != nil || sign <= 0 {
		return Metrics{}, fmt.Errorf("%w: starting capital must be positive", ErrInvalidMetrics)
	}
	if !params.FinalEquity.IsValid() || !params.FinalEquity.Currency().Equal(currency) {
		return Metrics{}, fmt.Errorf("%w: final equity must be valid money in %s", ErrInvalidMetrics, currency)
	}
	if !params.AccountFees.IsValid() || !params.AccountFees.Currency().Equal(currency) {
		return Metrics{}, fmt.Errorf("%w: account fees must be valid money in %s", ErrInvalidMetrics, currency)
	}

	var lastTS time.Time
	for i, p := range params.EquityCurve {
		if !p.Equity.IsValid() || !p.Equity.Currency().Equal(currency) {
			return Metrics{}, fmt.Errorf("%w: equity curve point %d must be valid money in %s", ErrInvalidMetrics, i, currency)
		}
		if i > 0 && p.Timestamp.Before(lastTS) {
			return Metrics{}, fmt.Errorf("%w: equity curve point %d timestamp precedes point %d", ErrInvalidMetrics, i, i-1)
		}
		lastTS = p.Timestamp
	}
	for i, t := range params.Trades {
		if !t.RealizedPnL.IsValid() || !t.Costs.IsValid() {
			return Metrics{}, fmt.Errorf("%w: trade %d has invalid realized pnl or costs", ErrInvalidMetrics, i)
		}
		if !t.RealizedPnL.Currency().Equal(currency) || !t.Costs.Currency().Equal(currency) {
			return Metrics{}, fmt.Errorf("%w: trade %d is denominated in a different currency than %s", ErrInvalidMetrics, i, currency)
		}
		if t.ClosedAt.IsZero() {
			return Metrics{}, fmt.Errorf("%w: trade %d has no ClosedAt — MetricsParams.Trades must be fully closed round trips", ErrInvalidMetrics, i)
		}
	}

	netReturn, err := params.FinalEquity.Sub(params.StartingCapital)
	if err != nil {
		return Metrics{}, fmt.Errorf("%w: computing net return: %v", ErrInvalidMetrics, err)
	}
	netReturnRate, err := netReturn.Div(params.StartingCapital)
	if err != nil {
		return Metrics{}, fmt.Errorf("%w: computing net return: %v", ErrInvalidMetrics, err)
	}

	m := Metrics{
		startingCapital: params.StartingCapital,
		finalEquity:     params.FinalEquity,
		netReturn:       netReturnRate,
		equityCurve:     append([]EquityPoint(nil), params.EquityCurve...),
		tradeCount:      len(params.Trades),
		accountFees:     params.AccountFees,
	}

	m.maxDrawdown, err = maxDrawdown(params.EquityCurve, currency)
	if err != nil {
		return Metrics{}, fmt.Errorf("%w: computing max drawdown: %v", ErrInvalidMetrics, err)
	}

	if err := m.computeTradeStats(params.Trades, currency); err != nil {
		return Metrics{}, err
	}
	m.perInstrument, err = perInstrumentMetrics(params.Trades, currency)
	if err != nil {
		return Metrics{}, err
	}
	m.bySide, err = sideMetrics(params.Trades, currency)
	if err != nil {
		return Metrics{}, err
	}

	return m, nil
}

// zeroMoneyMust returns zero Money in currency. currency is always
// already-validated by the time this is called (StartingCapital.
// Currency() of an already-IsValid Money), so the only possible error
// (an invalid currency) cannot occur here.
func zeroMoneyMust(currency num.Currency) num.Money {
	z, err := num.ParseMoney("0", currency)
	if err != nil {
		panic(fmt.Sprintf("backtest: unreachable: zero money for already-valid currency %s: %v", currency, err))
	}
	return z
}

// maxDrawdown walks curve tracking a running peak and returns the
// largest (peak-equity)/peak observed. An empty or single-point curve
// has nothing to decline from, so it returns zero. If the running peak
// is ever non-positive — a blown or negative account, pathological but
// representable — that point's drawdown is defined as 1 (100%) rather
// than dividing by a non-positive number.
func maxDrawdown(curve []EquityPoint, currency num.Currency) (num.Rate, error) {
	zero := num.Rate{}
	if len(curve) == 0 {
		return num.ParseRate("0")
	}
	peak := curve[0].Equity
	max, err := num.ParseRate("0")
	if err != nil {
		return zero, err
	}
	for _, p := range curve {
		if cmp, err := p.Equity.Cmp(peak); err == nil && cmp > 0 {
			peak = p.Equity
		}
		peakSign, err := peak.Cmp(zeroMoneyMust(currency))
		if err != nil {
			return zero, err
		}
		var dd num.Rate
		if peakSign <= 0 {
			dd, err = num.ParseRate("1")
			if err != nil {
				return zero, err
			}
		} else {
			decline, err := peak.Sub(p.Equity)
			if err != nil {
				return zero, err
			}
			dd, err = decline.Div(peak)
			if err != nil {
				return zero, err
			}
		}
		if cmp := dd.Cmp(max); cmp > 0 {
			max = dd
		}
	}
	return max, nil
}

// computeTradeStats fills in every closed-trade-derived field of m
// from trades, all denominated in currency.
func (m *Metrics) computeTradeStats(trades []order.Trade, currency num.Currency) error {
	zero := zeroMoneyMust(currency)
	grossPnL, totalCosts := zero, zero
	grossProfit, grossLoss := zero, zero
	var wins, losses int

	for _, t := range trades {
		var err error
		grossPnL, err = grossPnL.Add(t.RealizedPnL)
		if err != nil {
			return fmt.Errorf("%w: accumulating gross pnl: %v", ErrInvalidMetrics, err)
		}
		totalCosts, err = totalCosts.Add(t.Costs)
		if err != nil {
			return fmt.Errorf("%w: accumulating costs: %v", ErrInvalidMetrics, err)
		}

		net, err := t.RealizedPnL.Sub(t.Costs)
		if err != nil {
			return fmt.Errorf("%w: computing trade net result: %v", ErrInvalidMetrics, err)
		}
		sign, err := net.Cmp(zero)
		if err != nil {
			return fmt.Errorf("%w: comparing trade net result: %v", ErrInvalidMetrics, err)
		}
		switch {
		case sign > 0:
			wins++
			grossProfit, err = grossProfit.Add(net)
		case sign < 0:
			losses++
			grossLoss, err = grossLoss.Sub(net) // net is negative; grossLoss accumulates as positive magnitude
		}
		if err != nil {
			return fmt.Errorf("%w: accumulating win/loss totals: %v", ErrInvalidMetrics, err)
		}
	}

	netPnL, err := grossPnL.Sub(totalCosts)
	if err != nil {
		return fmt.Errorf("%w: computing net pnl: %v", ErrInvalidMetrics, err)
	}

	m.wins, m.losses = wins, losses
	m.grossPnL, m.closedTradeCosts, m.netPnL = grossPnL, totalCosts, netPnL

	if m.tradeCount > 0 {
		avg, err := netPnL.DivRate(mustRateFromInt(m.tradeCount))
		if err != nil {
			return fmt.Errorf("%w: computing expectancy: %v", ErrInvalidMetrics, err)
		}
		m.expectancy = &avg
	}
	if wins > 0 {
		avg, err := grossProfit.DivRate(mustRateFromInt(wins))
		if err != nil {
			return fmt.Errorf("%w: computing average win: %v", ErrInvalidMetrics, err)
		}
		m.averageWin = &avg
	}
	if losses > 0 {
		lossSum, err := zero.Sub(grossLoss)
		if err != nil {
			return fmt.Errorf("%w: computing average loss: %v", ErrInvalidMetrics, err)
		}
		avg, err := lossSum.DivRate(mustRateFromInt(losses))
		if err != nil {
			return fmt.Errorf("%w: computing average loss: %v", ErrInvalidMetrics, err)
		}
		m.averageLoss = &avg
	}
	if m.tradeCount > 0 {
		winRate, err := mustRateFromInt(wins).DivRate(mustRateFromInt(m.tradeCount))
		if err != nil {
			return fmt.Errorf("%w: computing win rate: %v", ErrInvalidMetrics, err)
		}
		m.winRate = &winRate
	}
	if !grossLoss.IsZero() {
		pf, err := grossProfit.Div(grossLoss)
		if err != nil {
			return fmt.Errorf("%w: computing profit factor: %v", ErrInvalidMetrics, err)
		}
		m.profitFactor = &pf
	}

	return nil
}

// mustRateFromInt returns n (a plain count, never a market value) as a
// dimensionless Rate, purely so it can be used as num.Money.DivRate's
// divisor to produce a per-count Money average, or as a Rate.DivRate
// operand for a plain ratio like win rate — n is always a small,
// exact, non-negative int here (a trade or win/loss count), so
// ParseRate cannot fail.
func mustRateFromInt(n int) num.Rate {
	r, err := num.ParseRate(fmt.Sprintf("%d", n))
	if err != nil {
		panic(fmt.Sprintf("backtest: unreachable: rate from int %d: %v", n, err))
	}
	return r
}

// perInstrumentMetrics groups trades by the same (instrument, provider,
// venue) key DeriveTrades itself uses, computing each group's own
// GrossPnL/Costs/NetPnL/win-loss counts. The result is sorted by that
// same key for deterministic output.
func perInstrumentMetrics(trades []order.Trade, currency num.Currency) ([]InstrumentMetrics, error) {
	zero := zeroMoneyMust(currency)
	groups := map[tradeKey]*InstrumentMetrics{}
	var order []tradeKey

	for _, t := range trades {
		key := keyForListing(t.Listing)
		g, ok := groups[key]
		if !ok {
			g = &InstrumentMetrics{
				InstrumentID: key.instrumentID,
				Provider:     key.provider,
				Venue:        key.venue,
				GrossPnL:     zero,
				Costs:        zero,
				NetPnL:       zero,
			}
			groups[key] = g
			order = append(order, key)
		}

		g.Count++
		var err error
		g.GrossPnL, err = g.GrossPnL.Add(t.RealizedPnL)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-instrument gross pnl: %v", ErrInvalidMetrics, err)
		}
		g.Costs, err = g.Costs.Add(t.Costs)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-instrument costs: %v", ErrInvalidMetrics, err)
		}
		net, err := t.RealizedPnL.Sub(t.Costs)
		if err != nil {
			return nil, fmt.Errorf("%w: computing per-instrument net result: %v", ErrInvalidMetrics, err)
		}
		g.NetPnL, err = g.NetPnL.Add(net)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-instrument net pnl: %v", ErrInvalidMetrics, err)
		}
		sign, err := net.Cmp(zero)
		if err != nil {
			return nil, fmt.Errorf("%w: comparing per-instrument net result: %v", ErrInvalidMetrics, err)
		}
		switch {
		case sign > 0:
			g.Wins++
		case sign < 0:
			g.Losses++
		}
	}

	sort.Slice(order, func(i, j int) bool { return lessTradeKey(order[i], order[j]) })
	out := make([]InstrumentMetrics, len(order))
	for i, key := range order {
		out[i] = *groups[key]
	}
	return out, nil
}

// sideMetrics groups trades by their own Side (Long or Short — never
// Flat, order.NewTrade's own invariant), computing each group's own
// GrossPnL/Costs/NetPnL/win-loss counts exactly as perInstrumentMetrics
// does for instruments. The result is ordered Long before Short (never
// map iteration order), and a side with no trades is omitted entirely
// rather than reported as an explicit zero — the same "absence means
// none" convention perInstrumentMetrics already uses for an instrument
// that was never traded.
//
// Any trade whose Side is not Long or Short is rejected outright (PR
// #265 review) rather than silently grouped and then dropped from the
// returned slice: this function's own documented sum-to-aggregate
// invariant depends on every trade landing in exactly one reported
// group, and a normal order.NewTrade-constructed population should
// never reach this branch in practice.
func sideMetrics(trades []order.Trade, currency num.Currency) ([]SideMetrics, error) {
	zero := zeroMoneyMust(currency)
	groups := map[order.PositionSide]*SideMetrics{}

	for i, t := range trades {
		if t.Side != order.Long && t.Side != order.Short {
			return nil, fmt.Errorf("%w: trade %d has side %s, want Long or Short", ErrInvalidMetrics, i, t.Side)
		}
		g, ok := groups[t.Side]
		if !ok {
			g = &SideMetrics{Side: t.Side, GrossPnL: zero, Costs: zero, NetPnL: zero}
			groups[t.Side] = g
		}

		g.Count++
		var err error
		g.GrossPnL, err = g.GrossPnL.Add(t.RealizedPnL)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-side gross pnl: %v", ErrInvalidMetrics, err)
		}
		g.Costs, err = g.Costs.Add(t.Costs)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-side costs: %v", ErrInvalidMetrics, err)
		}
		net, err := t.RealizedPnL.Sub(t.Costs)
		if err != nil {
			return nil, fmt.Errorf("%w: computing per-side net result: %v", ErrInvalidMetrics, err)
		}
		g.NetPnL, err = g.NetPnL.Add(net)
		if err != nil {
			return nil, fmt.Errorf("%w: accumulating per-side net pnl: %v", ErrInvalidMetrics, err)
		}
		sign, err := net.Cmp(zero)
		if err != nil {
			return nil, fmt.Errorf("%w: comparing per-side net result: %v", ErrInvalidMetrics, err)
		}
		switch {
		case sign > 0:
			g.Wins++
		case sign < 0:
			g.Losses++
		}
	}

	var out []SideMetrics
	if g, ok := groups[order.Long]; ok {
		out = append(out, *g)
	}
	if g, ok := groups[order.Short]; ok {
		out = append(out, *g)
	}
	return out, nil
}

// lessTradeKey gives a deterministic total order over tradeKey values,
// matching the same (instrument, provider, venue) precedence
// trades.go's own lessOpenTrade already established.
func lessTradeKey(a, b tradeKey) bool {
	if a.instrumentID != b.instrumentID {
		return a.instrumentID.String() < b.instrumentID.String()
	}
	if a.provider != b.provider {
		return a.provider < b.provider
	}
	return a.venue < b.venue
}

// StartingCapital is the run's starting account equity.
func (m Metrics) StartingCapital() num.Money { return m.startingCapital }

// FinalEquity is the run's final account equity.
func (m Metrics) FinalEquity() num.Money { return m.finalEquity }

// NetReturn is (FinalEquity-StartingCapital)/StartingCapital.
func (m Metrics) NetReturn() num.Rate { return m.netReturn }

// MaxDrawdown is the largest peak-to-trough decline observed across
// EquityCurve, as a fraction of the peak. Zero for an empty or
// single-point curve.
func (m Metrics) MaxDrawdown() num.Rate { return m.maxDrawdown }

// EquityCurve returns a defensive copy of the authoritative,
// mark-to-market equity series this Metrics was built from.
func (m Metrics) EquityCurve() []EquityPoint {
	return append([]EquityPoint(nil), m.equityCurve...)
}

// TradeCount is the number of fully closed round trips.
func (m Metrics) TradeCount() int { return m.tradeCount }

// Wins is the number of closed trades with a positive net result
// (RealizedPnL - Costs).
func (m Metrics) Wins() int { return m.wins }

// Losses is the number of closed trades with a negative net result. A
// trade whose net result is exactly zero counts toward TradeCount but
// neither Wins nor Losses.
func (m Metrics) Losses() int { return m.losses }

// WinRate is Wins/TradeCount. Nil when TradeCount is zero: 0/0 is
// undefined, and a strategy that never traded should not be reported
// as having won 0% of its (nonexistent) trades.
func (m Metrics) WinRate() *num.Rate { return m.winRate }

// AverageWin is the mean net result across winning trades. Nil when
// Wins is zero.
func (m Metrics) AverageWin() *num.Money { return m.averageWin }

// AverageLoss is the mean net result across losing trades, negative-
// signed (a loss), not an absolute magnitude. Nil when Losses is zero.
func (m Metrics) AverageLoss() *num.Money { return m.averageLoss }

// GrossPnL is the sum of RealizedPnL across every closed trade, before
// costs.
func (m Metrics) GrossPnL() num.Money { return m.grossPnL }

// ClosedTradeCosts is the sum of Costs across every closed trade —
// not the run's total incurred costs, which also includes entry
// commission already paid on any still-open position. See
// AccountFees for that authoritative, run-level figure.
func (m Metrics) ClosedTradeCosts() num.Money { return m.closedTradeCosts }

// AccountFees is the run's authoritative, account-level cumulative
// fee total (Result.Account.Fees()) — unlike ClosedTradeCosts, it
// includes costs already incurred on any position still open when
// the run ended.
func (m Metrics) AccountFees() num.Money { return m.accountFees }

// NetPnL is GrossPnL minus ClosedTradeCosts.
func (m Metrics) NetPnL() num.Money { return m.netPnL }

// Expectancy is the mean net result per closed trade (NetPnL /
// TradeCount). Nil when TradeCount is zero.
func (m Metrics) Expectancy() *num.Money { return m.expectancy }

// ProfitFactor is gross profit divided by gross loss (as a positive
// magnitude). Nil when gross loss is zero, including when there are no
// losing trades — division by zero has no sane ratio, and an
// "infinite" profit factor is a worse API than an explicit absence.
func (m Metrics) ProfitFactor() *num.Rate { return m.profitFactor }

// PerInstrument returns a defensive copy of the per-instrument
// breakdown, sorted deterministically. Each entry's GrossPnL/Costs/
// NetPnL sum back to the corresponding aggregate accessor across every
// entry, since both are computed from the same Trades population.
func (m Metrics) PerInstrument() []InstrumentMetrics {
	return append([]InstrumentMetrics(nil), m.perInstrument...)
}

// BySide returns a defensive copy of the run's own per-side (Long/
// Short) closed-trade breakdown, ordered Long before Short — issue
// #254 (EMA-09)'s own "long/short breakdown" requirement, computed
// here (not in report) so report can remain a pure projection over an
// existing accessor, matching every other TradeStats/InstrumentReport
// field's own convention.
func (m Metrics) BySide() []SideMetrics {
	return append([]SideMetrics(nil), m.bySide...)
}
