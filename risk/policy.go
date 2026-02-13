package risk

import "time"

type Policy struct {
	AccountBaseCurrency string  // "USD"
	AccountStartBalance float64 // e.g. 1000

	// Risk limits
	DefaultRiskPct float64 // 0.005
	MaxRiskPct     float64 // 0.01

	// Circuit breakers
	MaxDailyLossPct  float64 // 0.015
	MaxWeeklyLossPct float64 // 0.03

	// Exposure limits
	MaxOpenTrades int     // 3
	MaxMarginPct  float64 // 0.20

	// Trade constraints
	MinRR float64 // 1.5
}

type TradeIntent struct {
	Now        time.Time
	Instrument string // "EUR_USD" or "EUR/USD"
	Units      float64

	Entry      float64
	Stop       float64
	TakeProfit float64

	// For correlation buckets (optional)
	RiskBucket string // e.g. "USD" or "EUR" or "majors"
}

type AccountSnapshot struct {
	Balance float64
	Equity  float64

	// Broker-provided or computed
	MarginUsed  float64
	MarginAvail float64

	OpenTrades int
}

type PnLSnapshot struct {
	DayRealized  float64 // realized P/L for day in account currency
	WeekRealized float64 // realized P/L for week
}
