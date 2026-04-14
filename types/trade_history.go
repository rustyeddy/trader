package types

type TradeHistory struct {
	*TradeCommon
	*portfolio.Request
}

func NewTradeHistory(inst string) *TradeHistory {
	return &TradeHistory{
		TradeCommon: &TradeCommon{
			ID:         NewULID(),
			Instrument: inst,
		},
	}
}
