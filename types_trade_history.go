package trader

type TradeHistory struct {
	*TradeCommon
	*OpenRequest
}

func NewTradeHistory(inst string) *TradeHistory {
	return &TradeHistory{
		TradeCommon: &TradeCommon{
			ID:         NewULID(),
			Instrument: inst,
		},
	}
}
