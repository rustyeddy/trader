package trader

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whole JSON, with the values a run of the engine uses: what a client reads.
func TestRunSeries_SummaryReportsTheSettingsTheRunUsed(t *testing.T) {
	captureLogs(t)

	res, err := RunSeries(context.Background(), "x", time.Time{}, wave("EURUSD", 1, upThenDown...))
	require.NoError(t, err)
	got, err := json.Marshal(res.Summary().Settings)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"risk_per_trade": "0.01",
		"max_order_notional": {"amount": "50000000", "currency": "USD"},
		"margin_closeout_level": "0.5",
		"instruments": [
			{"instrument": "EURUSD", "margin_rate": "0.02", "min_units": "1", "unit_step": "1"},
			{"instrument": "GBPUSD", "margin_rate": "0.05", "min_units": "1", "unit_step": "1"}
		]
	}`, string(got))
}

// Non-default settings are the ones reported, because they are read from what
// sized the order.
func TestNewResult_ReportsNonDefaultSettings(t *testing.T) {
	captureLogs(t)
	br := newBroker("sim", testMarkets())
	act := br.addAccount("a", usd(10000))
	act.riskPct = num.MustParseRate("0.002")
	act.maxOrderNotional = usd(1_000_000)
	m := act.markets.byKey["EURUSD"]
	m.rules.minUnits, m.rules.maxUnits = num.Quantity{}, qty(500_000)
	o := brokerOpen("a")
	require.NoError(t, br.submit(context.Background(), o))

	res, err := newResult("x", 0, &trader{broker: br, account: act})
	require.NoError(t, err)
	got := (&Result{r: res}).Summary().Settings

	assert.Equal(t, num.MustParseRate("0.002"), got.RiskPerTrade)
	assert.Equal(t, usd(1_000_000), got.MaxOrderNotional)
	require.Len(t, got.Instruments, 2)
	assert.Equal(t, InstrumentLimits{Instrument: "EURUSD", MarginRate: rateTwoPercent,
		MinUnits: oneUnit, UnitStep: oneUnit, MaxUnits: qty(500_000)}, got.Instruments[0])
	// 0.2% of 10,000 is 20, and 20 over the 0.011 stop distance is 1818.18 units;
	// the default 1% would have sized 9090.
	assert.Equal(t, qty(1818), o.units, "the order was sized by the reported risk")
}

func TestSettingsOf_ACustomMarginPolicyHasNoCloseoutLevel(t *testing.T) {
	br := newBroker("sim", testMarkets())
	act := br.addAccount("a", usd(10000))
	br.margin = marginLevelPolicy(rateOne, rateOne)

	got := settingsOf(br, act)

	assert.Nil(t, got.MarginCloseoutLevel, "the default level would not be the one applied")
	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "margin_closeout_level")
}
