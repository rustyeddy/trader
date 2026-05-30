package trader

// TradeHistory defines the TradeHistory type.
type TradeHistory struct {
	*TradeCommon
	*OpenRequest
}

// NewTradeHistory performs NewTradeHistory.
func NewTradeHistory(inst string) *TradeHistory {
	return &TradeHistory{
		TradeCommon: &TradeCommon{
			ID:         NewULID(),
			Instrument: inst,
		},
	}
}
