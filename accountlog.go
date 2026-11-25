package trader

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// The kinds of account observation: the state before the first traded frame, and
// the state after each traded frame.
const (
	observeInitial = "initial"
	observeFrame   = "frame"
)

// accountLine is one line of the account journal (account.jsonl): the account as it
// stood when an observation was made. Money is exact, in the account's currency,
// and the line carries simulated time only, so two runs that trade the same way write
// the same lines.
//
// The first line is kind "initial", at the opening time of the first traded frame,
// before anything has traded. Every later line is kind "frame", at that frame's
// availability, after its fills, marks and margin rules have settled. A frame holds
// the instruments that have a bar at that instant, and a position whose instrument
// has none is not marked again: it keeps its previous mark, and its instrument is
// listed in Stale, so an unchanged value is told from a fresh one. Nothing is
// carried forward silently.
type accountLine struct {
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	Account    string    `json:"account"`
	Balance    num.Money `json:"balance"`
	Unrealized num.Money `json:"unrealized"`
	Equity     num.Money `json:"equity"`
	Open       int       `json:"open"`            // open positions
	Stale      []string  `json:"stale,omitempty"` // open positions not marked in this frame, by instrument
}

// accountLog records the account after each traded frame and, from the same
// observations, tracks the run's maximum equity drawdown. It is not a second
// accounting engine: every figure is read from the account (equity, balance,
// unrealized P&L) at the moment of the observation. The drawdown is exactly what a
// reader gets by replaying account.jsonl (see Drawdown).
//
// The writer is optional: without one the run still has its drawdown, and the
// engine opens no file.
type accountLog struct {
	w   io.Writer
	seq int

	initial, last num.Money // equity of the first and the latest observation

	peak     num.Money
	peakTime time.Time
	dd       Drawdown
}

// observe records the account at t. stale lists the open instruments the frame did
// not mark.
func (l *accountLog) observe(kind string, t time.Time, act *account, stale []string) error {
	unrealized, err := act.unrealizedPnL()
	if err != nil {
		return fmt.Errorf("account observation: %w", err)
	}
	equity, err := act.balance.Add(unrealized)
	if err != nil {
		return fmt.Errorf("account observation: %w", err)
	}
	l.seq++
	t = t.UTC()
	if l.w != nil {
		line, err := json.Marshal(accountLine{Seq: l.seq, Time: t, Kind: kind, Account: act.id,
			Balance: act.balance, Unrealized: unrealized, Equity: equity, Open: len(act.positions), Stale: stale})
		if err != nil {
			return fmt.Errorf("account journal: %w", err)
		}
		line = append(line, '\n')
		n, err := l.w.Write(line)
		if err == nil && n < len(line) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return fmt.Errorf("account journal: %w", err)
		}
	}
	return l.track(t, equity)
}

// track applies one observation's equity to the peak and the drawdown.
func (l *accountLog) track(t time.Time, equity num.Money) error {
	l.last = equity
	if l.seq == 1 {
		l.initial, l.peak, l.peakTime = equity, equity, t
		zero, err := zeroMoneyOf(equity.Currency())
		if err != nil {
			return err
		}
		l.dd = Drawdown{Amount: zero, PeakEquity: equity, TroughEquity: equity, PeakTime: t, TroughTime: t}
		return nil
	}
	if c, err := equity.Cmp(l.peak); err != nil {
		return err
	} else if c > 0 {
		l.peak, l.peakTime = equity, t
	}
	depth, err := l.peak.Sub(equity)
	if err != nil {
		return err
	}
	if c, err := depth.Cmp(l.dd.Amount); err != nil {
		return err
	} else if c <= 0 {
		return nil // only a strictly deeper drawdown replaces the first one found
	}
	sign, err := moneySign(l.peak)
	if err != nil {
		return err
	}
	if sign <= 0 {
		return fmt.Errorf("account observation: drawdown from a peak equity of %s has no percentage", l.peak)
	}
	pct, err := percent(depth, l.peak)
	if err != nil {
		return err
	}
	l.dd = Drawdown{Amount: depth, Pct: pct, PeakEquity: l.peak, TroughEquity: equity, PeakTime: l.peakTime, TroughTime: t}
	return nil
}

// netReturnPct is the final equity over the initial equity, in percent, or false
// when nothing was observed.
func (l *accountLog) netReturnPct() (num.Rate, bool, error) {
	if l.seq == 0 {
		return num.Rate{}, false, nil
	}
	gain, err := l.last.Sub(l.initial)
	if err != nil {
		return num.Rate{}, false, err
	}
	pct, err := percent(gain, l.initial)
	return pct, true, err
}

// percent is part as a percentage of whole, to the eight places of a Rate: the
// part is scaled by 100 first, so the percentage is rounded once.
func percent(part, whole num.Money) (num.Rate, error) {
	hundred, err := part.MulRate(num.MustParseRate("100"))
	if err != nil {
		return num.Rate{}, err
	}
	return hundred.Div(whole)
}

// staleInstruments are the instruments of act's open positions that f has no bar for.
func staleInstruments(act *account, f frame) []string {
	var stale []string
	for _, sym := range act.positionSymbols() {
		if !slices.ContainsFunc(f.bars, func(b marketdata.Bar) bool { return b.Instrument == sym }) {
			stale = append(stale, sym)
		}
	}
	return stale
}
