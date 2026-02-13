package risk

import (
	"fmt"
	"time"
)

type Violation struct {
	Code string
	Msg  string
}

type Decision struct {
	Allowed    bool
	Violations []Violation

	PlannedRiskUSD float64
	PlannedRiskPct float64
	PlannedRR      float64
}

func (d *Decision) add(code, msg string) {
	d.Violations = append(d.Violations, Violation{Code: code, Msg: msg})
	d.Allowed = false
}

func Evaluate(
	p Policy,
	intent TradeIntent,
	acct AccountSnapshot,
	pnl PnLSnapshot,
	quoteToAccountRate float64, // for EUR/USD in USD acct: 1.0
) Decision {
	d := Decision{Allowed: true}

	// Basic sanity
	if intent.Stop == 0 || intent.Entry == 0 {
		d.add("NO_STOP_OR_ENTRY", "entry/stop must be set")
		return d
	}
	if intent.Units == 0 {
		d.add("NO_UNITS", "units must be non-zero")
		return d
	}

	// Risk + RR
	d.PlannedRiskUSD = PlannedRiskUSD(intent.Units, intent.Entry, intent.Stop, quoteToAccountRate)
	d.PlannedRiskPct = RiskPct(d.PlannedRiskUSD, acct.Equity)
	d.PlannedRR = RR(intent.Entry, intent.Stop, intent.TakeProfit)

	if d.PlannedRiskPct > p.MaxRiskPct {
		d.add("RISK_TOO_HIGH",
			fmt.Sprintf("planned risk %.2f%% exceeds max %.2f%%",
				100*d.PlannedRiskPct, 100*p.MaxRiskPct))
	}
	if d.PlannedRiskPct > p.DefaultRiskPct {
		// Not a hard fail by itself if <= MaxRiskPct; you can choose:
		// either warn-only, or enforce as "must justify".
		// Here: make it a violation so it forces explicit override logic in caller.
		d.add("RISK_OVER_DEFAULT",
			fmt.Sprintf("planned risk %.2f%% exceeds default %.2f%% (requires override)",
				100*d.PlannedRiskPct, 100*p.DefaultRiskPct))
	}
	if d.PlannedRR < p.MinRR {
		d.add("RR_TOO_LOW",
			fmt.Sprintf("RR %.2f below minimum %.2f", d.PlannedRR, p.MinRR))
	}

	// Exposure constraints
	if acct.OpenTrades >= p.MaxOpenTrades {
		d.add("TOO_MANY_OPEN_TRADES",
			fmt.Sprintf("open trades %d >= max %d", acct.OpenTrades, p.MaxOpenTrades))
	}

	// Margin cap
	if acct.Equity > 0 && acct.MarginUsed/acct.Equity > p.MaxMarginPct {
		d.add("MARGIN_TOO_HIGH",
			fmt.Sprintf("margin used %.2f%% exceeds max %.2f%%",
				100*(acct.MarginUsed/acct.Equity), 100*p.MaxMarginPct))
	}

	// Circuit breakers (loss limits)
	dayLimit := -p.MaxDailyLossPct * acct.Equity
	if pnl.DayRealized <= dayLimit {
		d.add("DAILY_LOSS_LIMIT", fmt.Sprintf("day realized %.2f <= limit %.2f", pnl.DayRealized, dayLimit))
	}
	weekLimit := -p.MaxWeeklyLossPct * acct.Equity
	if pnl.WeekRealized <= weekLimit {
		d.add("WEEKLY_LOSS_LIMIT", fmt.Sprintf("week realized %.2f <= limit %.2f", pnl.WeekRealized, weekLimit))
	}

	// Time-based rules (optional hook)
	_ = time.UTC // placeholder; you can add "no trading outside X session" later

	return d
}
