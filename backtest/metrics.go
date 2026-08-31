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
	// Trades are the run's fully closed round trips (Result.Trades).
	// Every closed-trade statistic below — GrossPnL, TotalCosts,
	// NetPnL, win/loss counts, expectancy, profit factor, and the
	// per-instrument breakdown — is computed from this population
	// only. Still-open positions have no final win/loss outcome yet,
	// so they are deliberately not accepted here at all; account-level
	// figures (FinalEquity, NetReturn) already reflect their
	// unrealized contribution through the authoritative EquityCurve/
	// FinalEquity instead.
	Trades []order.Trade
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

	tradeCount                   int
	wins, losses                 int
	winRate                      *num.Rate
	averageWin, averageLoss      *num.Money
	grossPnL, totalCosts, netPnL num.Money
	expectancy                   *num.Money
	profitFactor                 *num.Rate

	perInstrument []InstrumentMetrics
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
		if !t.RealizedPnL.Currency().Equal(currency) || !t.Costs.Currency().Equal(currency) {
			return Metrics{}, fmt.Errorf("%w: trade %d is denominated in a different currency than %s", ErrInvalidMetrics, i, currency)
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
	m.grossPnL, m.totalCosts, m.netPnL = grossPnL, totalCosts, netPnL

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

// TotalCosts is the sum of Costs across every closed trade.
func (m Metrics) TotalCosts() num.Money { return m.totalCosts }

// NetPnL is GrossPnL minus TotalCosts.
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
