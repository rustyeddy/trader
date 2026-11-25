package trader

import "github.com/rustyeddy/trader/num"

// Settings are the risk and account settings a run was sized and admitted under,
// read from the account, broker and markets the engine used. They are facts about
// the run: no client sets them.
type Settings struct {
	// RiskPerTrade is the fraction of equity ("0.01" is 1%) an open is sized to lose
	// at its stop. It is planned risk, not a cap on the loss.
	RiskPerTrade num.Rate `json:"risk_per_trade"`
	// MaxOrderNotional is the largest notional one opening order may have.
	MaxOrderNotional num.Money `json:"max_order_notional"`
	// MarginCloseoutLevel is the margin level, equity as a fraction of the margin in
	// use, below which the broker liquidates (and refuses opens decided on that
	// frame). It is absent when the broker ran a margin policy of its own, which no
	// single level describes.
	MarginCloseoutLevel *num.Rate `json:"margin_closeout_level,omitempty"`
	// Instruments are the limits of every market the run's engine could size an
	// order for, in instrument order; it can hold more than the run's instruments.
	Instruments []InstrumentLimits `json:"instruments"`
}

// InstrumentLimits are one market's limits on the size of an order.
type InstrumentLimits struct {
	Instrument string `json:"instrument"`
	// MarginRate is the fraction of a position's notional held as margin.
	MarginRate num.Rate `json:"margin_rate"`
	// MinUnits is the smallest size an open may have and UnitStep the step a size is
	// rounded down to.
	MinUnits num.Quantity `json:"min_units"`
	UnitStep num.Quantity `json:"unit_step"`
	// MaxUnits is the largest size of one order, absent when there is no limit.
	MaxUnits num.Quantity `json:"max_units,omitzero"`
}

// settingsOf reads the settings in effect for the account on the broker.
func settingsOf(br *broker, act *account) Settings {
	s := Settings{
		RiskPerTrade:     act.riskPct,
		MaxOrderNotional: act.maxOrderNotional,
		Instruments:      []InstrumentLimits{},
	}
	if br.margin == nil { // the broker applies defaultMarginPolicy
		level := defaultCloseoutLevel
		s.MarginCloseoutLevel = &level
	}
	for _, key := range act.markets.keys() {
		m := act.markets.byKey[key]
		s.Instruments = append(s.Instruments, InstrumentLimits{
			Instrument: key,
			MarginRate: m.rules.marginRate,
			MinUnits:   m.minUnits(),
			UnitStep:   m.unitStep,
			MaxUnits:   m.rules.maxUnits,
		})
	}
	return s
}
