package report

import (
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// BacktestReport is a report-owned projection of a backtest.Result
// (issue #220, M5-12). NewBacktestReport builds one by copying fields
// and calling existing accessors only — it performs no arithmetic, no
// currency conversion, and no re-derivation of anything backtest.
// Metrics or backtest.DeriveTrades already computed. Renderers consume
// only this type, never backtest.Result or backtest.Metrics directly,
// so a future backtest-internal change cannot silently change what a
// renderer emits, and this type's own JSON tags — not backtest's field
// names — are report's wire contract (issue #220 review, point 3).
//
// Every timestamp is normalized to UTC (review point 6's "canonical
// timestamp format") — a presentation re-expression of the same
// instant, not a computed value. Every instrument identity is carried
// as its canonical string form (instrument.ID.String()), matching the
// convention marketdata.Manifest's own wire structs already use, since
// instrument.ID has no JSON representation of its own.
type BacktestReport struct {
	Run           RunInfo             `json:"run"`
	Performance   Performance         `json:"performance"`
	TradeStats    TradeStats          `json:"trade_stats"`
	PerInstrument []InstrumentReport  `json:"per_instrument"`
	ClosedTrades  []TradeReport       `json:"closed_trades"`
	OpenTrades    []TradeReport       `json:"open_trades"`
	Account       AccountReport       `json:"account"`
	EquityCurve   []EquityPointReport `json:"equity_curve"`
}

// RunInfo identifies and dates the run this report describes.
type RunInfo struct {
	RunID           string    `json:"run_id"`
	StrategyName    string    `json:"strategy_name"`
	StrategyVersion string    `json:"strategy_version,omitempty"`
	SpanStart       time.Time `json:"span_start"`
	SpanEnd         time.Time `json:"span_end"`
	TraderVersion   string    `json:"trader_version,omitempty"`
	ConfigDigest    string    `json:"config_digest"`
}

// Performance is the run's account-level bookend figures — exactly
// backtest.Metrics' own StartingCapital/FinalEquity/NetReturn/
// MaxDrawdown, copied without modification.
type Performance struct {
	StartingCapital num.Money `json:"starting_capital"`
	FinalEquity     num.Money `json:"final_equity"`
	NetReturn       num.Rate  `json:"net_return"`
	MaxDrawdown     num.Rate  `json:"max_drawdown"`
}

// TradeStats is the run's closed-trade statistics — exactly backtest.
// Metrics' own accessors, copied without modification. A nil pointer
// here means backtest.Metrics itself judged the figure undefined (for
// example zero closed trades), and renders as "n/a" (Org/text) or
// null (JSON) rather than a fabricated zero.
type TradeStats struct {
	TradeCount       int        `json:"trade_count"`
	Wins             int        `json:"wins"`
	Losses           int        `json:"losses"`
	WinRate          *num.Rate  `json:"win_rate"`
	AverageWin       *num.Money `json:"average_win"`
	AverageLoss      *num.Money `json:"average_loss"`
	GrossPnL         num.Money  `json:"gross_pnl"`
	ClosedTradeCosts num.Money  `json:"closed_trade_costs"`
	AccountFees      num.Money  `json:"account_fees"`
	NetPnL           num.Money  `json:"net_pnl"`
	Expectancy       *num.Money `json:"expectancy"`
	ProfitFactor     *num.Rate  `json:"profit_factor"`
}

// InstrumentReport is one instrument's slice of TradeStats, copied
// field-for-field from backtest.InstrumentMetrics.
type InstrumentReport struct {
	Instrument string    `json:"instrument"`
	Provider   string    `json:"provider,omitempty"`
	Venue      string    `json:"venue,omitempty"`
	Count      int       `json:"count"`
	Wins       int       `json:"wins"`
	Losses     int       `json:"losses"`
	GrossPnL   num.Money `json:"gross_pnl"`
	Costs      num.Money `json:"costs"`
	NetPnL     num.Money `json:"net_pnl"`
}

// TradeReport is one order.Trade, copied field-for-field. ClosedAt is
// the Go zero time for an open trade (order.Trade's own documented
// convention) — renderers must check IsZero rather than assume every
// TradeReport is closed. No net-per-trade figure is included: backtest
// never exposed one, and computing RealizedPnL-Costs here would be
// report re-deriving a value, not merely projecting it (issue #220
// review).
type TradeReport struct {
	Instrument  string    `json:"instrument"`
	Provider    string    `json:"provider,omitempty"`
	Venue       string    `json:"venue,omitempty"`
	Side        string    `json:"side"`
	OpenedAt    time.Time `json:"opened_at"`
	ClosedAt    time.Time `json:"closed_at,omitzero"`
	RealizedPnL num.Money `json:"realized_pnl"`
	Costs       num.Money `json:"costs"`
}

// PositionReport is one open order.Position, copied field-for-field.
// AvgPrice is nil exactly when Side is Flat (order.Position's own
// invariant).
type PositionReport struct {
	Instrument string       `json:"instrument"`
	Provider   string       `json:"provider,omitempty"`
	Venue      string       `json:"venue,omitempty"`
	Side       string       `json:"side"`
	Quantity   num.Quantity `json:"quantity"`
	AvgPrice   *num.Price   `json:"avg_price"`
}

// AccountReport is the run's final account.Snapshot, copied field-for-
// field. OpenOrderCount is a count rather than full order detail: a
// completed backtest run does not normally end with working orders,
// and full order detail is not part of what this issue's acceptance
// criteria asks a backtest report to show.
type AccountReport struct {
	AccountID       string           `json:"account_id"`
	Broker          string           `json:"broker,omitempty"`
	Currency        num.Currency     `json:"currency"`
	AsOf            time.Time        `json:"as_of"`
	Equity          num.Money        `json:"equity"`
	CashBalances    []num.Money      `json:"cash_balances"`
	BuyingPower     num.Money        `json:"buying_power"`
	MarginUsed      num.Money        `json:"margin_used"`
	MarginAvailable num.Money        `json:"margin_available"`
	RealizedPnL     num.Money        `json:"realized_pnl"`
	UnrealizedPnL   num.Money        `json:"unrealized_pnl"`
	Fees            num.Money        `json:"fees"`
	Financing       num.Money        `json:"financing"`
	OpenPositions   []PositionReport `json:"open_positions"`
	OpenOrderCount  int              `json:"open_order_count"`
}

// EquityPointReport is one backtest.EquityPoint, copied field-for-
// field with its Timestamp normalized to UTC.
type EquityPointReport struct {
	Timestamp time.Time `json:"timestamp"`
	Equity    num.Money `json:"equity"`
}

// NewBacktestReport projects result into a BacktestReport. It performs
// no computation: every field is either copied directly from an
// existing accessor or converted to its canonical string/UTC form.
func NewBacktestReport(result backtest.Result) BacktestReport {
	m := result.Manifest
	metrics := result.Metrics

	perInstrument := make([]InstrumentReport, len(metrics.PerInstrument()))
	for i, im := range metrics.PerInstrument() {
		perInstrument[i] = toInstrumentReport(im)
	}

	closed := make([]TradeReport, len(result.Trades))
	for i, t := range result.Trades {
		closed[i] = toTradeReport(t)
	}
	open := make([]TradeReport, len(result.OpenTrades))
	for i, t := range result.OpenTrades {
		open[i] = toTradeReport(t)
	}

	curve := make([]EquityPointReport, len(result.EquityCurve))
	for i, p := range result.EquityCurve {
		curve[i] = EquityPointReport{Timestamp: p.Timestamp.UTC(), Equity: p.Equity}
	}

	return BacktestReport{
		Run: RunInfo{
			RunID:           m.RunID().String(),
			StrategyName:    m.StrategyName(),
			StrategyVersion: m.StrategyVersion(),
			SpanStart:       m.Span().Start().UTC(),
			SpanEnd:         m.Span().End().UTC(),
			TraderVersion:   m.TraderVersion(),
			ConfigDigest:    m.ConfigDigest(),
		},
		Performance: Performance{
			StartingCapital: metrics.StartingCapital(),
			FinalEquity:     metrics.FinalEquity(),
			NetReturn:       metrics.NetReturn(),
			MaxDrawdown:     metrics.MaxDrawdown(),
		},
		TradeStats: TradeStats{
			TradeCount:       metrics.TradeCount(),
			Wins:             metrics.Wins(),
			Losses:           metrics.Losses(),
			WinRate:          metrics.WinRate(),
			AverageWin:       metrics.AverageWin(),
			AverageLoss:      metrics.AverageLoss(),
			GrossPnL:         metrics.GrossPnL(),
			ClosedTradeCosts: metrics.ClosedTradeCosts(),
			AccountFees:      metrics.AccountFees(),
			NetPnL:           metrics.NetPnL(),
			Expectancy:       metrics.Expectancy(),
			ProfitFactor:     metrics.ProfitFactor(),
		},
		PerInstrument: perInstrument,
		ClosedTrades:  closed,
		OpenTrades:    open,
		Account:       toAccountReport(result.Account),
		EquityCurve:   curve,
	}
}

func toInstrumentReport(im backtest.InstrumentMetrics) InstrumentReport {
	return InstrumentReport{
		Instrument: im.InstrumentID.String(),
		Provider:   im.Provider,
		Venue:      im.Venue,
		Count:      im.Count,
		Wins:       im.Wins,
		Losses:     im.Losses,
		GrossPnL:   im.GrossPnL,
		Costs:      im.Costs,
		NetPnL:     im.NetPnL,
	}
}

func toTradeReport(t order.Trade) TradeReport {
	tr := TradeReport{
		Instrument:  t.Listing.InstrumentID().String(),
		Provider:    t.Listing.Provider(),
		Venue:       t.Listing.Venue(),
		Side:        t.Side.String(),
		OpenedAt:    t.OpenedAt.UTC(),
		RealizedPnL: t.RealizedPnL,
		Costs:       t.Costs,
	}
	if !t.ClosedAt.IsZero() {
		tr.ClosedAt = t.ClosedAt.UTC()
	}
	return tr
}

func toAccountReport(s account.Snapshot) AccountReport {
	positions := s.Positions()
	openPositions := make([]PositionReport, 0, len(positions))
	for _, p := range positions {
		if p.Side == order.Flat {
			continue
		}
		openPositions = append(openPositions, PositionReport{
			Instrument: p.Listing.InstrumentID().String(),
			Provider:   p.Listing.Provider(),
			Venue:      p.Listing.Venue(),
			Side:       p.Side.String(),
			Quantity:   p.Quantity,
			AvgPrice:   p.AvgPrice,
		})
	}

	return AccountReport{
		AccountID:       s.AccountID().String(),
		Broker:          s.Broker(),
		Currency:        s.Currency(),
		AsOf:            s.AsOf().UTC(),
		Equity:          s.Equity(),
		CashBalances:    s.CashBalances(),
		BuyingPower:     s.BuyingPower(),
		MarginUsed:      s.MarginUsed(),
		MarginAvailable: s.MarginAvailable(),
		RealizedPnL:     s.RealizedPnL(),
		UnrealizedPnL:   s.UnrealizedPnL(),
		Fees:            s.Fees(),
		Financing:       s.Financing(),
		OpenPositions:   openPositions,
		OpenOrderCount:  len(s.OpenOrders()),
	}
}
